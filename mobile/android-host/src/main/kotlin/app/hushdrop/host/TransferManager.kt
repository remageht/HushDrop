package app.hushdrop.host

import android.content.Context
import android.util.Log
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.RandomAccessFile
import java.net.URLDecoder
import java.security.MessageDigest
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.locks.ReentrantReadWriteLock
import kotlin.concurrent.read
import kotlin.concurrent.write

data class HostFileMetadata(
    val id: String,
    val originalName: String,
    val cleanName: String,
    val size: Long,
    val mimeType: String,
    val totalChunks: Int,
    var uploadedChunks: Int,
    var sha256: String,
    val isDangerous: Boolean,
    var encrypted: Boolean = false,
    var isCompleted: Boolean,
    val createdAt: String,
    var filePath: File? = null,
    var tempFilePath: File? = null
)

data class HostUploadStatus(
    val id: String,
    val originalName: String,
    val cleanName: String,
    val size: Long,
    val totalChunks: Int,
    val uploadedChunks: Int,
    val receivedChunks: List<Int>,
    val isCompleted: Boolean,
    val isDangerous: Boolean,
    val sha256: String
)

/**
 * Result of TransferManager.openDownload: the (possibly decrypting) stream plus the
 * backing file. The caller must close the stream.
 */
class OpenedDownload(val stream: InputStream, val file: File, val encrypted: Boolean)

class TransferManager(
    private val context: Context,
    val maxChunkSize: Long = 4L * 1024 * 1024,          // 4MB
    val maxFileSize: Long = 5L * 1024 * 1024 * 1024,    // 5GB
    atRestKey: ByteArray? = null
) {
    private val lock = ReentrantReadWriteLock()

    // Non-null (cloned exactly like Go's bytes.Clone) enables at-rest encryption of
    // every uploaded file; mirrors transfer.NewManagerWithEncryption.
    private val atRestKeyBytes: ByteArray? = atRestKey?.copyOf()

    private val baseDir = File(context.filesDir, "hushdrop_host").apply { mkdirs() }
    private val downloadsDir = File(baseDir, "downloads").apply { mkdirs() }
    private val tempDir = File(baseDir, "temp").apply { mkdirs() }

    private val files = ConcurrentHashMap<String, HostFileMetadata>()
    private val chunkTrackers = ConcurrentHashMap<String, MutableSet<Int>>()

    private val safeIdRegex = Regex("^[a-zA-Z0-9_-]{4,64}$")
    private val dangerousExtensions = setOf(
        ".exe", ".bat", ".cmd", ".ps1", ".sh", ".vbs", ".msi",
        ".com", ".scr", ".reg", ".jar", ".dll", ".sys"
    )

    companion object {
        private const val TAG = "TransferManager"

        /**
         * Creates a manager with at-rest encryption enabled: loads or generates the
         * master key at filesDir/hushdrop_host/atrest.key (0600-equivalent app-private
         * storage, mirroring Go's data/atrest.key). Never log the key bytes themselves.
         */
        fun withEncryption(context: Context): TransferManager {
            val keyFile = File(File(context.filesDir, "hushdrop_host"), AtRestCrypto.KEY_FILE_NAME)
            val key = AtRestCrypto.loadOrCreateAtRestKey(keyFile)
            Log.i(TAG, "[SECURITY] At-rest encryption enabled. Key file: ${keyFile.path}")
            Log.i(TAG, "[SECURITY] Do not store the key outside of a trusted device - without it the data cannot be recovered")
            return TransferManager(context, atRestKey = key)
        }

        fun sanitizeFilename(rawName: String): String {
            var name = try {
                URLDecoder.decode(rawName, "UTF-8")
            } catch (_: Exception) {
                rawName
            }
            name = name.replace('\\', '/')
            val base = name.substringAfterLast('/')
            val cleaned = base.replace("\u0000", "")
                .replace("..", "")
                .replace("/", "")
                .replace("\\", "")
                // Strip control characters (CR/LF/TAB etc.) to prevent header injection
                .filter { it.code >= 0x20 && it.code != 0x7F }
                .trim()
            return if (cleaned.isEmpty() || cleaned == ".") "unnamed_file" else cleaned
        }

        fun isDangerousFile(fileName: String): Boolean {
            val dotIdx = fileName.lastIndexOf('.')
            if (dotIdx == -1) return false
            val ext = fileName.substring(dotIdx).lowercase(Locale.ROOT)
            return ext in setOf(
                ".exe", ".bat", ".cmd", ".ps1", ".sh", ".vbs", ".msi",
                ".com", ".scr", ".reg", ".jar", ".dll", ".sys"
            )
        }

        fun formatIsoDate(date: Date = Date()): String {
            val sdf = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss'Z'", Locale.US)
            sdf.timeZone = TimeZone.getTimeZone("UTC")
            return sdf.format(date)
        }

        fun sha256Hex(file: File): String {
            val md = MessageDigest.getInstance("SHA-256")
            FileInputStream(file).use { fis ->
                val buf = ByteArray(64 * 1024)
                var read: Int
                while (fis.read(buf).also { read = it } != -1) {
                    md.update(buf, 0, read)
                }
            }
            return md.digest().joinToString("") { String.format(Locale.US, "%02x", it) }
        }
    }

    fun initUpload(
        fileId: String,
        rawName: String,
        totalSize: Long,
        totalChunks: Int,
        mimeType: String = "application/octet-stream"
    ): HostFileMetadata = lock.write {
        if (!safeIdRegex.matches(fileId)) {
            throw IllegalArgumentException("invalid file id: must be alphanumeric and between 4 and 64 characters")
        }

        if (totalSize <= 0 || totalSize > maxFileSize) {
            throw IllegalArgumentException("invalid declared file size $totalSize: must be between 1 and $maxFileSize bytes")
        }

        // Chunk count must correspond exactly to the declared size at max chunk granularity
        val expectedChunks = ((totalSize + maxChunkSize - 1) / maxChunkSize).toInt()
        if (totalChunks != expectedChunks) {
            throw IllegalArgumentException("chunk count mismatch: declared $totalSize bytes needs exactly $expectedChunks chunks, got $totalChunks")
        }

        val cleanName = sanitizeFilename(rawName)
        val isDangerous = isDangerousFile(cleanName)

        val prefix = if (fileId.length > 8) fileId.take(8) else fileId
        val tempFile = File(tempDir, "$fileId.part")
        val finalFile = File(downloadsDir, "${prefix}_$cleanName")

        // Directory traversal escape prevention
        val canonicalTemp = tempFile.canonicalPath
        val canonicalFinal = finalFile.canonicalPath
        if (!canonicalTemp.startsWith(tempDir.canonicalPath)) {
            throw SecurityException("temp path escape detected")
        }
        if (!canonicalFinal.startsWith(downloadsDir.canonicalPath)) {
            throw SecurityException("destination path escape detected")
        }

        val existing = files[fileId]
        if (existing != null && existing.size == totalSize && existing.cleanName == cleanName) {
            if (existing.isCompleted && existing.filePath?.exists() == true) {
                return existing
            }
            if (tempFile.exists()) {
                return existing
            }
        }

        if (tempFile.exists()) {
            tempFile.delete()
        }
        tempFile.createNewFile()

        val meta = HostFileMetadata(
            id = fileId,
            originalName = rawName,
            cleanName = cleanName,
            size = totalSize,
            mimeType = mimeType,
            totalChunks = totalChunks,
            uploadedChunks = 0,
            sha256 = "",
            isDangerous = isDangerous,
            isCompleted = false,
            createdAt = formatIsoDate(),
            filePath = finalFile,
            tempFilePath = tempFile
        )

        files[fileId] = meta
        chunkTrackers[fileId] = mutableSetOf()
        meta
    }

    fun writeChunk(
        fileId: String,
        chunkIndex: Int,
        chunkSize: Long,
        chunkStream: InputStream
    ): HostFileMetadata {
        if (chunkSize > maxChunkSize) {
            throw IllegalArgumentException("chunk size $chunkSize exceeds limit of $maxChunkSize bytes")
        }

        val meta = files[fileId] ?: throw IllegalArgumentException("upload session not initialized")
        val tempFile = meta.tempFilePath ?: throw IllegalStateException("temp file missing")

        if (chunkIndex < 0 || chunkIndex >= meta.totalChunks) {
            throw IllegalArgumentException("chunk index $chunkIndex out of range (total chunks ${meta.totalChunks})")
        }
        val offset = chunkIndex.toLong() * maxChunkSize
        if (offset < 0 || offset >= meta.size) {
            throw IllegalArgumentException("chunk offset $offset beyond declared file size ${meta.size}")
        }

        // Idempotently acknowledge retried chunks after finalization without rewriting the file
        lock.read { if (meta.isCompleted) return meta.copy() }

        // Stream chunk into temp file at calculated offset using 64KB buffer
        RandomAccessFile(tempFile, "rw").use { raf ->
            raf.seek(offset)
            val buffer = ByteArray(64 * 1024)
            var totalRead = 0L
            while (totalRead < chunkSize) {
                val toRead = minOf(buffer.size.toLong(), chunkSize - totalRead).toInt()
                val read = chunkStream.read(buffer, 0, toRead)
                if (read == -1) break
                raf.write(buffer, 0, read)
                totalRead += read
            }
            if (chunkSize > 0 && totalRead != chunkSize) {
                throw java.io.IOException("incomplete chunk write: $totalRead of $chunkSize bytes received")
            }
        }

        return lock.write {
            val current = files[fileId]
            if (current == null || current !== meta) {
                throw java.lang.IllegalStateException("upload session expired or revoked")
            }
            // Re-check under lock: a concurrent request may have finalized already
            if (meta.isCompleted) return@write meta.copy()

            val tracker = chunkTrackers.getOrPut(fileId) { mutableSetOf() }
            if (tracker.add(chunkIndex)) {
                meta.uploadedChunks = tracker.size
            }

            if (meta.uploadedChunks >= meta.totalChunks) {
                finalizeUpload(meta, tracker)
            }

            meta.copy()
        }
    }

    private fun finalizeUpload(meta: HostFileMetadata, tracker: Set<Int>) {
        if (tracker.size != meta.totalChunks) {
            throw IllegalStateException("incomplete upload: received ${tracker.size} chunks out of ${meta.totalChunks}")
        }
        for (i in 0 until meta.totalChunks) {
            if (!tracker.contains(i)) {
                throw IllegalStateException("missing chunk $i before finalization")
            }
        }

        val tempFile = meta.tempFilePath ?: return
        val finalFile = meta.filePath ?: return

        // Truncate to exact size
        RandomAccessFile(tempFile, "rw").use { raf ->
            raf.setLength(meta.size)
        }

        // Calculate SHA-256
        val md = MessageDigest.getInstance("SHA-256")
        FileInputStream(tempFile).use { fis ->
            val buf = ByteArray(64 * 1024)
            var read: Int
            while (fis.read(buf).also { read = it } != -1) {
                md.update(buf, 0, read)
            }
        }
        val hashBytes = md.digest()
        meta.sha256 = hashBytes.joinToString("") { String.format(Locale.US, "%02x", it) }

        meta.encrypted = atRestKeyBytes != null
        if (atRestKeyBytes != null) {
            encryptToFinal(meta)
        } else {
            // Atomic move
            if (finalFile.exists()) finalFile.delete()
            if (!tempFile.renameTo(finalFile)) {
                tempFile.copyTo(finalFile, overwrite = true)
                tempFile.delete()
            }
        }

        meta.isCompleted = true
    }

    /**
     * Streams the plaintext temp file into an at-rest encrypted container at the final
     * destination (mirror of transfer.encryptToFinalLocked). SHA-256 above always
     * refers to the plaintext. A failed container write leaves nothing behind.
     */
    private fun encryptToFinal(meta: HostFileMetadata) {
        val tempFile = meta.tempFilePath ?: throw IOException("temp file missing")
        val finalFile = meta.filePath ?: throw IOException("final path missing")
        val key = atRestKeyBytes ?: throw IOException("at-rest key not available")

        try {
            FileOutputStream(finalFile).use { fos ->
                FileInputStream(tempFile).use { fis ->
                    AtRestCrypto.encryptStream(fis, fos, key)
                }
            }

            // Cheap structural validation: the container must announce itself and be
            // larger than the plaintext by the framing overhead.
            if (finalFile.length() <= meta.size) {
                throw IOException(
                    "container size ${finalFile.length()} suspiciously small for ${meta.size} plaintext bytes"
                )
            }
            if (!AtRestCrypto.isEncrypted(finalFile)) {
                throw IOException("written container misses the at-rest magic header")
            }
            tempFile.delete()
        } catch (e: Exception) {
            finalFile.delete()
            throw IOException("at-rest encryption of ${meta.cleanName} failed: ${e.message}", e)
        }
    }

    /**
     * Opens a completed file for reading; encrypted files come back as a streaming
     * decryptor over the at-rest container (mirror of transfer.OpenDownload).
     * The caller owns the returned stream; closing it closes the underlying file.
     */
    fun openDownload(meta: HostFileMetadata): OpenedDownload {
        val file = meta.filePath ?: throw IOException("file path missing")
        val input = FileInputStream(file)
        if (!meta.encrypted) {
            return OpenedDownload(input, file, false)
        }
        val key = atRestKeyBytes
        if (key == null) {
            try { input.close() } catch (_: Exception) {}
            throw IOException("file is encrypted but the at-rest key is not available")
        }
        return try {
            OpenedDownload(AtRestCrypto.decryptingInputStream(input, key), file, true)
        } catch (e: Exception) {
            try { input.close() } catch (_: Exception) {}
            throw e
        }
    }

    fun listFiles(): List<HostFileMetadata> = lock.read {
        files.values.filter { it.isCompleted }.map { it.copy() }
    }

    fun getFile(fileId: String): HostFileMetadata? = lock.read {
        val meta = files[fileId] ?: return null
        if (!meta.isCompleted) return null
        meta
    }

    fun getUploadStatus(fileId: String): HostUploadStatus? = lock.read {
        val meta = files[fileId] ?: return null
        val tracker = chunkTrackers[fileId]
        val chunks = tracker?.toList()?.sorted() ?: emptyList()
        HostUploadStatus(
            id = meta.id,
            originalName = meta.originalName,
            cleanName = meta.cleanName,
            size = meta.size,
            totalChunks = meta.totalChunks,
            uploadedChunks = meta.uploadedChunks,
            receivedChunks = chunks,
            isCompleted = meta.isCompleted,
            isDangerous = meta.isDangerous,
            sha256 = meta.sha256
        )
    }

    fun addHostedFile(file: File, displayName: String, mimeType: String = "application/octet-stream"): HostFileMetadata = lock.write {
        val fileId = "host_${System.currentTimeMillis()}_${(1000..9999).random()}"
        val cleanName = sanitizeFilename(displayName)
        val isDangerous = isDangerousFile(cleanName)

        val destinationFile = File(downloadsDir, "${fileId.take(8)}_$cleanName")

        // The SHA-256 and size always refer to the plaintext (mirrors Go); in encrypted
        // mode the source is streamed into an at-rest container at the destination.
        val sha256 = sha256Hex(file)
        val plainSize = file.length()
        val totalChunks = ((plainSize + maxChunkSize - 1) / maxChunkSize).toInt().coerceAtLeast(1)

        val key = atRestKeyBytes
        if (key != null) {
            try {
                FileOutputStream(destinationFile).use { fos ->
                    FileInputStream(file).use { fis ->
                        AtRestCrypto.encryptStream(fis, fos, key)
                    }
                }
                if (!AtRestCrypto.isEncrypted(destinationFile)) {
                    throw IOException("written container misses the at-rest magic header")
                }
            } catch (e: Exception) {
                destinationFile.delete()
                throw IOException("at-rest encryption of $cleanName failed: ${e.message}", e)
            }
        } else {
            file.copyTo(destinationFile, overwrite = true)
        }

        val meta = HostFileMetadata(
            id = fileId,
            originalName = displayName,
            cleanName = cleanName,
            size = plainSize,
            mimeType = mimeType,
            totalChunks = totalChunks,
            uploadedChunks = totalChunks,
            sha256 = sha256,
            isDangerous = isDangerous,
            encrypted = key != null,
            isCompleted = true,
            createdAt = formatIsoDate(),
            filePath = destinationFile,
            tempFilePath = null
        )

        files[fileId] = meta
        meta
    }

    fun clearAll() = lock.write {
        for ((_, meta) in files) {
            meta.tempFilePath?.delete()
            meta.filePath?.delete()
        }
        files.clear()
        chunkTrackers.clear()

        tempDir.deleteRecursively()
        downloadsDir.deleteRecursively()
        tempDir.mkdirs()
        downloadsDir.mkdirs()
    }
}
