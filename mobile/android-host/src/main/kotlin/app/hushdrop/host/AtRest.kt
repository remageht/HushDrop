package app.hushdrop.host

import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.security.SecureRandom
import javax.crypto.AEADBadTagException
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

/**
 * At-rest container format (V2, streaming) — byte-for-byte mirror of
 * `internal/crypto/atrest.go`:
 *
 *     magic      16B  "HUSHDROP_ENC_V2\n"
 *     baseNonce   8B  random per file; frame nonce = baseNonce || BE32(counter), counter starts at 1
 *     segLen      4B  BE u32, maximum plaintext bytes per frame (4 MiB)
 *     frames: data BE u32 plainLen (1..segLen) || AES-256-GCM(key, nonce, plaintext, aad=magic) [16B tag]
 *             eof   BE u32 0                 || AES-256-GCM(key, nonce, <empty>,    aad=magic) [16B tag]
 *
 * The eof frame authenticates the end of the plaintext stream, so a truncated container
 * fails instead of silently returning a prefix. Trailing data after the eof frame is
 * rejected. Every frame is bound to the magic header (AAD), so frames cannot be
 * transplanted across files or formats.
 */
object AtRestCrypto {
    val MAGIC_BYTES: ByteArray = "HUSHDROP_ENC_V2\n".toByteArray(Charsets.US_ASCII) // exactly 16 bytes

    const val BASE_NONCE_SIZE = 8
    const val SEGMENT_SIZE = 4 * 1024 * 1024
    const val FRAME_HEADER_SIZE = 4
    const val KEY_SIZE = 32
    const val GCM_NONCE_SIZE = 12
    const val GCM_TAG_BITS = 128
    const val KEY_FILE_NAME = "atrest.key"

    class AtRestInvalidHeaderException(message: String) : IOException(message)
    class AtRestCorruptFrameException(message: String) : IOException(message)
    class AtRestInvalidKeyException(message: String) : IOException(message)
    class AtRestWriteAfterCloseException(message: String) : IOException(message)

    // --- key handling ---------------------------------------------------------

    /** Returns the 32-byte key stored at [file], creating a fresh random key file on first use. */
    fun loadOrCreateAtRestKey(file: File): ByteArray {
        val parent = file.parentFile
        if (parent != null && !parent.exists()) {
            parent.mkdirs()
        }
        if (file.createNewFile()) {
            val key = ByteArray(KEY_SIZE).also { SecureRandom().nextBytes(it) }
            try {
                FileOutputStream(file).use { it.write(key) }
            } catch (e: Exception) {
                file.delete()
                throw IOException("at-rest key write failed: ${e.message}", e)
            }
            return key
        }
        val existing = file.readBytes()
        if (existing.size != KEY_SIZE) {
            throw AtRestInvalidKeyException(
                "at-rest key file ${file.path} has wrong size ${existing.size}, expected $KEY_SIZE bytes"
            )
        }
        return existing
    }

    /** Cheap structural check: does the file start with the V2 magic header? */
    fun isEncrypted(file: File): Boolean {
        if (!file.exists() || file.length() < MAGIC_BYTES.size) return false
        FileInputStream(file).use { ins ->
            val header = ByteArray(MAGIC_BYTES.size)
            var got = 0
            while (got < header.size) {
                val n = ins.read(header, got, header.size - got)
                if (n == -1) return false
                got += n
            }
            return header.contentEquals(MAGIC_BYTES)
        }
    }

    internal fun keySpec(key: ByteArray): SecretKeySpec {
        if (key.size != KEY_SIZE) {
            throw AtRestInvalidKeyException("invalid at-rest key length ${key.size}, expected $KEY_SIZE")
        }
        return SecretKeySpec(key, "AES")
    }

    internal fun putBe32(buf: ByteArray, off: Int, value: Long) {
        val v = value and 0xFFFFFFFFL
        buf[off] = ((v ushr 24) and 0xFF).toByte()
        buf[off + 1] = ((v ushr 16) and 0xFF).toByte()
        buf[off + 2] = ((v ushr 8) and 0xFF).toByte()
        buf[off + 3] = (v and 0xFF).toByte()
    }

    internal fun getBe32(buf: ByteArray, off: Int): Long {
        return ((buf[off].toLong() and 0xFF) shl 24) or
                ((buf[off + 1].toLong() and 0xFF) shl 16) or
                ((buf[off + 2].toLong() and 0xFF) shl 8) or
                (buf[off + 3].toLong() and 0xFF)
    }

    internal fun frameNonce(base: ByteArray, counter: Long): ByteArray {
        val nonce = ByteArray(GCM_NONCE_SIZE)
        System.arraycopy(base, 0, nonce, 0, BASE_NONCE_SIZE)
        putBe32(nonce, BASE_NONCE_SIZE, counter)
        return nonce
    }

    // --- streams --------------------------------------------------------------

    fun encryptingOutputStream(dst: OutputStream, key: ByteArray): OutputStream =
        AtRestOutputStream(dst, key)

    fun decryptingInputStream(src: InputStream, key: ByteArray): InputStream =
        AtRestInputStream(src, key)

    /** Streams all of [src] into [dst] as a V2 at-rest container. */
    fun encryptStream(src: InputStream, dst: OutputStream, key: ByteArray) {
        val w = encryptingOutputStream(dst, key)
        val buf = ByteArray(64 * 1024)
        try {
            while (true) {
                val n = src.read(buf)
                if (n == -1) break
                if (n > 0) w.write(buf, 0, n)
            }
            w.close()
        } catch (e: Exception) {
            try { w.close() } catch (_: Exception) {}
            throw IOException("at-rest encrypt stream failed: ${e.message}", e)
        }
    }

    /** Streams the decrypted plaintext of a V2 at-rest container from [src] into [dst]. */
    fun decryptStream(src: InputStream, dst: OutputStream, key: ByteArray) {
        val r = decryptingInputStream(src, key)
        val buf = ByteArray(64 * 1024)
        try {
            while (true) {
                val n = r.read(buf)
                if (n == -1) break
                if (n > 0) dst.write(buf, 0, n)
            }
        } finally {
            r.close()
        }
    }
}

/**
 * Encrypting stream: buffers up to a full segment (4 MiB), emits [len] + ciphertext frames
 * and an authenticated eof frame on close. Header is written eagerly in the constructor.
 */
class AtRestOutputStream(private val dst: OutputStream, key: ByteArray) : OutputStream() {

    private val secret = AtRestCrypto.keySpec(key)
    private val base = ByteArray(AtRestCrypto.BASE_NONCE_SIZE).also { SecureRandom().nextBytes(it) }
    private val buffer = ByteArray(AtRestCrypto.SEGMENT_SIZE)
    private var fill = 0
    private var counter = 1L
    private var closed = false
    private var errored = false

    init {
        // Container header: magic || baseNonce || segLen — self-describing from byte 0.
        val headerLen = AtRestCrypto.MAGIC_BYTES.size + AtRestCrypto.BASE_NONCE_SIZE + AtRestCrypto.FRAME_HEADER_SIZE
        val header = ByteArray(headerLen)
        System.arraycopy(AtRestCrypto.MAGIC_BYTES, 0, header, 0, AtRestCrypto.MAGIC_BYTES.size)
        System.arraycopy(base, 0, header, AtRestCrypto.MAGIC_BYTES.size, AtRestCrypto.BASE_NONCE_SIZE)
        AtRestCrypto.putBe32(header, AtRestCrypto.MAGIC_BYTES.size + AtRestCrypto.BASE_NONCE_SIZE, AtRestCrypto.SEGMENT_SIZE.toLong())
        try {
            dst.write(header)
        } catch (e: Exception) {
            errored = true
            throw IOException("at-rest header write failed: ${e.message}", e)
        }
    }

    override fun write(b: Int) {
        write(byteArrayOf(b.toByte()), 0, 1)
    }

    override fun write(b: ByteArray, off: Int, len: Int) {
        if (errored) throw IOException("at-rest writer in failed state")
        if (closed) {
            errored = true
            throw AtRestCrypto.AtRestWriteAfterCloseException("write after close")
        }
        if (len == 0) return
        if (off < 0 || len < 0 || off + len > b.size) throw IndexOutOfBoundsException("off=$off len=$len size=${b.size}")
        var pos = off
        var remaining = len
        while (remaining > 0) {
            val free = AtRestCrypto.SEGMENT_SIZE - fill
            if (free == 0) {
                flushFrame(AtRestCrypto.SEGMENT_SIZE)
                continue
            }
            val take = minOf(free, remaining)
            System.arraycopy(b, pos, buffer, fill, take)
            fill += take
            pos += take
            remaining -= take
        }
    }

    override fun close() {
        if (closed) return
        closed = true
        if (errored) return
        try {
            if (fill > 0) {
                flushFrame(fill)
                fill = 0
            }
            flushFrame(0) // authenticated eof marker
            dst.flush()
        } catch (e: Exception) {
            errored = true
            throw e
        }
    }

    private fun flushFrame(plainLen: Int) {
        if (counter > 0xFFFFFFFFL) {
            errored = true
            throw IOException("at-rest frame counter exhausted")
        }
        try {
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            cipher.init(Cipher.ENCRYPT_MODE, secret, GCMParameterSpec(AtRestCrypto.GCM_TAG_BITS, AtRestCrypto.frameNonce(base, counter)))
            cipher.updateAAD(AtRestCrypto.MAGIC_BYTES)
            val ct = cipher.doFinal(buffer, 0, plainLen)

            val lenScr = ByteArray(AtRestCrypto.FRAME_HEADER_SIZE)
            AtRestCrypto.putBe32(lenScr, 0, plainLen.toLong())
            dst.write(lenScr)
            dst.write(ct)
            counter++
            fill = 0
        } catch (e: IOException) {
            errored = true
            throw e
        } catch (e: Exception) {
            errored = true
            throw IOException("at-rest frame seal failed: ${e.message}", e)
        }
    }
}

/**
 * Decrypting stream over a V2 at-rest container. Keeps at most one frame of plaintext
 * in memory. Frame authentication failures surface as AtRestInvalidKeyException;
 * abrupt truncation and trailing junk as AtRestCorruptFrameException.
 */
class AtRestInputStream(private val source: InputStream, key: ByteArray) : InputStream() {

    private val secret = AtRestCrypto.keySpec(key)
    private val base = ByteArray(AtRestCrypto.BASE_NONCE_SIZE)
    private var segmentLen = 0L
    private var counter = 1L
    private var plain: ByteArray = ByteArray(0)
    private var offset = 0
    private var done = false
    private var errored = false

    init {
        val header = ByteArray(AtRestCrypto.MAGIC_BYTES.size + AtRestCrypto.BASE_NONCE_SIZE + AtRestCrypto.FRAME_HEADER_SIZE)
        var got = 0
        while (got < header.size) {
            val n = source.read(header, got, header.size - got)
            if (n == -1) break
            got += n
        }
        if (got < header.size) {
            throw AtRestCrypto.AtRestInvalidHeaderException("at-rest stream shorter than the ${header.size}-byte header")
        }
        for (i in AtRestCrypto.MAGIC_BYTES.indices) {
            if (header[i] != AtRestCrypto.MAGIC_BYTES[i]) {
                throw AtRestCrypto.AtRestInvalidHeaderException("invalid hushdrop at-rest encryption header")
            }
        }
        System.arraycopy(header, AtRestCrypto.MAGIC_BYTES.size, base, 0, AtRestCrypto.BASE_NONCE_SIZE)
        segmentLen = AtRestCrypto.getBe32(header, AtRestCrypto.MAGIC_BYTES.size + AtRestCrypto.BASE_NONCE_SIZE)
        if (segmentLen == 0L || segmentLen > AtRestCrypto.SEGMENT_SIZE) {
            throw AtRestCrypto.AtRestCorruptFrameException("at-rest stream declares invalid segment length $segmentLen")
        }
    }

    override fun read(): Int {
        val one = ByteArray(1)
        val n = read(one, 0, 1)
        return if (n == 1) (one[0].toInt() and 0xFF) else -1
    }

    override fun read(b: ByteArray, off: Int, len: Int): Int {
        if (errored) throw IOException("at-rest reader in failed state")
        if (len == 0) return 0
        while (offset >= plain.size) {
            if (done) return -1
            try {
                nextFrame()
            } catch (e: Exception) {
                errored = true
                throw e
            }
        }
        val take = minOf((plain.size - offset).toLong(), len.toLong()).toInt()
        System.arraycopy(plain, offset, b, off, take)
        offset += take
        return take
    }

    private fun nextFrame() {
        val lenScr = ByteArray(AtRestCrypto.FRAME_HEADER_SIZE)
        var got = 0
        while (got < lenScr.size) {
            val n = source.read(lenScr, got, lenScr.size - got)
            if (n == -1) {
                throw AtRestCrypto.AtRestCorruptFrameException("truncated at-rest stream (missing frame)")
            }
            got += n
        }
        val plainLen = AtRestCrypto.getBe32(lenScr, 0)
        if (plainLen > segmentLen) {
            throw AtRestCrypto.AtRestCorruptFrameException("at-rest frame length $plainLen exceeds segment length $segmentLen")
        }

        val boxed = ByteArray((plainLen + 16L).toInt())
        var bgot = 0
        while (bgot < boxed.size) {
            val n = source.read(boxed, bgot, boxed.size - bgot)
            if (n == -1) {
                throw AtRestCrypto.AtRestCorruptFrameException("truncated at-rest stream (partial frame)")
            }
            bgot += n
        }

        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, secret, GCMParameterSpec(AtRestCrypto.GCM_TAG_BITS, AtRestCrypto.frameNonce(base, counter)))
        cipher.updateAAD(AtRestCrypto.MAGIC_BYTES)
        val decoded: ByteArray = try {
            cipher.doFinal(boxed)
        } catch (_: AEADBadTagException) {
            throw AtRestCrypto.AtRestInvalidKeyException("invalid at-rest key or corrupted data")
        }
        counter++

        if (decoded.isEmpty()) {
            // Authenticated end-of-stream marker: refuse trailing data.
            val probe = source.read()
            if (probe != -1) {
                throw AtRestCrypto.AtRestCorruptFrameException("trailing data after the eof marker")
            }
            done = true
            plain = ByteArray(0)
            offset = 0
            return
        }

        plain = decoded
        offset = 0
    }

    override fun close() {
        source.close()
    }
}
