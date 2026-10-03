package app.hushdrop.host

import android.content.Context
import java.io.File
import java.io.FileInputStream
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
    var isCompleted: Boolean,
    val createdAt: String,
    var filePath: File? = null,
    var tempFilePath: File? = null
)

class TransferManager(
    private val context: Context,
    val maxChunkSize: Long = 4L * 1024 * 1024,          // 4MB
    val maxFileSize: Long = 5L * 1024 * 1024 * 1024      // 5GB
) {
    private val lock = ReentrantReadWriteLock()

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

        if (totalSize > maxFileSize) {
            throw IllegalArgumentException("file size $totalSize exceeds max allowed $maxFileSize bytes")
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

        val offset = chunkIndex.toLong() * maxChunkSize

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
        }

        lock.write {
            val tracker = chunkTrackers.getOrPut(fileId) { mutableSetOf() }
            if (tracker.add(chunkIndex)) {
                meta.uploadedChunks = tracker.size
            }

            if (meta.uploadedChunks >= meta.totalChunks) {
                finalizeUpload(meta, tracker)
            }
        }

        return meta
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

        // Atomic move
        if (finalFile.exists()) finalFile.delete()
        if (!tempFile.renameTo(finalFile)) {
            tempFile.copyTo(finalFile, overwrite = true)
            tempFile.delete()
        }

        meta.isCompleted = true
    }

    fun listFiles(): List<HostFileMetadata> = lock.read {
        files.values.filter { it.isCompleted }.map { it.copy() }
    }

    fun getFile(fileId: String): HostFileMetadata? = lock.read {
        val meta = files[fileId] ?: return null
        if (!meta.isCompleted) return null
        meta
    }

    fun addHostedFile(file: File, displayName: String, mimeType: String = "application/octet-stream"): HostFileMetadata = lock.write {
        val fileId = "host_${System.currentTimeMillis()}_${(1000..9999).random()}"
        val cleanName = sanitizeFilename(displayName)
        val isDangerous = isDangerousFile(cleanName)

        val destinationFile = File(downloadsDir, "${fileId.take(8)}_$cleanName")
        file.copyTo(destinationFile, overwrite = true)

        // Calculate SHA-256
        val md = MessageDigest.getInstance("SHA-256")
        FileInputStream(destinationFile).use { fis ->
            val buf = ByteArray(64 * 1024)
            var read: Int
            while (fis.read(buf).also { read = it } != -1) {
                md.update(buf, 0, read)
            }
        }
        val sha256 = md.digest().joinToString("") { String.format(Locale.US, "%02x", it) }
        val totalChunks = ((destinationFile.length() + maxChunkSize - 1) / maxChunkSize).toInt().coerceAtLeast(1)

        val meta = HostFileMetadata(
            id = fileId,
            originalName = displayName,
            cleanName = cleanName,
            size = destinationFile.length(),
            mimeType = mimeType,
            totalChunks = totalChunks,
            uploadedChunks = totalChunks,
            sha256 = sha256,
            isDangerous = isDangerous,
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
