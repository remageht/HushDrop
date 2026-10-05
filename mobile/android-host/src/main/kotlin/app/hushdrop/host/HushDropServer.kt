package app.hushdrop.host

import android.content.Context
import android.util.Log
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedInputStream
import java.io.BufferedOutputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.FileInputStream
import java.io.InputStream
import java.io.OutputStream
import java.io.RandomAccessFile
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.net.URLDecoder
import java.nio.charset.StandardCharsets
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import javax.net.ssl.SSLServerSocket

class HushDropServer(
    private val context: Context,
    private val tlsBundle: HostTlsBundle,
    val pairManager: PairManager,
    val transferManager: TransferManager,
    val httpsPort: Int = 8443,
    val httpPort: Int = 8080
) {
    private val tag = "HushDropServer"
    private val isRunning = AtomicBoolean(false)
    private val threadPool = Executors.newCachedThreadPool()

    private var httpsServerSocket: SSLServerSocket? = null
    private var httpServerSocket: ServerSocket? = null

    private var clipboardBytes: ByteArray? = null
    private var clipboardBurnAfterRead: Boolean = false
    private var clipboardUpdatedAt: Long = 0L

    companion object {
        fun maskIp(ip: String): String {
            val parts = ip.split(".")
            return if (parts.size == 4) {
                "${parts[0]}.${parts[1]}.***.***"
            } else {
                "***"
            }
        }

        fun maskFilename(filename: String): String {
            return if (filename.length <= 4) {
                "***"
            } else {
                filename.take(2) + "***" + filename.takeLast(4)
            }
        }

        fun isPrivateIp(addr: InetAddress): Boolean {
            if (addr.isLoopbackAddress || addr.isSiteLocalAddress || addr.isLinkLocalAddress) {
                return true
            }
            val ip = addr.hostAddress ?: return false
            // RFC 1918 IPv4
            if (ip.startsWith("192.168.") || ip.startsWith("10.")) return true
            if (ip.startsWith("172.")) {
                val parts = ip.split(".")
                if (parts.size >= 2) {
                    val second = parts[1].toIntOrNull()
                    if (second != null && second in 16..31) return true
                }
            }
            // IPv6 link-local or ULA
            if (ip.startsWith("fe80:") || ip.startsWith("fc00:") || ip.startsWith("fd00:")) return true
            return false
        }
    }

    fun start() {
        if (isRunning.getAndSet(true)) return

        // 1. Start HTTPS on 8443
        threadPool.execute {
            try {
                val ssf = tlsBundle.sslContext.serverSocketFactory
                val server = ssf.createServerSocket(httpsPort) as SSLServerSocket
                httpsServerSocket = server
                Log.i(tag, "HTTPS Server listening on port $httpsPort")

                while (isRunning.get()) {
                    try {
                        val clientSocket = server.accept()
                        threadPool.execute { handleClientConnection(clientSocket, isHttps = true) }
                    } catch (e: Exception) {
                        if (!isRunning.get()) break
                        Log.e(tag, "HTTPS accept error: ${e.message}")
                    }
                }
            } catch (e: Exception) {
                Log.e(tag, "Failed to start HTTPS server on port $httpsPort", e)
            }
        }

        // 2. Start HTTP on 8080 (helper & cert download)
        threadPool.execute {
            try {
                val server = ServerSocket(httpPort)
                httpServerSocket = server
                Log.i(tag, "HTTP Helper Server listening on port $httpPort")

                while (isRunning.get()) {
                    try {
                        val clientSocket = server.accept()
                        threadPool.execute { handleClientConnection(clientSocket, isHttps = false) }
                    } catch (e: Exception) {
                        if (!isRunning.get()) break
                        Log.e(tag, "HTTP accept error: ${e.message}")
                    }
                }
            } catch (e: Exception) {
                Log.e(tag, "Failed to start HTTP server on port $httpPort", e)
            }
        }
    }

    fun stop() {
        if (!isRunning.getAndSet(false)) return
        try {
            httpsServerSocket?.close()
        } catch (_: Exception) {}
        try {
            httpServerSocket?.close()
        } catch (_: Exception) {}
        threadPool.shutdownNow()
        pairManager.shutdown()
        Log.i(tag, "HushDropServer stopped")
    }

    private fun handleClientConnection(socket: Socket, isHttps: Boolean) {
        val clientIp = socket.inetAddress?.hostAddress ?: "unknown"

        try {
            socket.soTimeout = 30_000 // 30 sec timeout

            // 1. LAN-only security check (RFC 1918)
            val clientAddr = socket.inetAddress
            if (clientAddr == null || !isPrivateIp(clientAddr)) {
                Log.w(tag, "[SECURITY] Dropped non-LAN connection from ${maskIp(clientIp)}")
                val out = socket.getOutputStream()
                sendResponse(out, 403, "Forbidden", "application/json", "{\"error\":\"Forbidden: LAN access only\"}")
                socket.close()
                return
            }

            // 2. Rate limit check (20/min per IP)
            if (!pairManager.allowRateLimit(clientIp)) {
                Log.w(tag, "[RATE_LIMIT] Throttled masked IP ${maskIp(clientIp)}")
                val out = socket.getOutputStream()
                val headers = listOf("Retry-After: 60")
                sendResponse(
                    out, 429, "Too Many Requests", "application/json",
                    "{\"error\":\"Too many requests. Please slow down.\"}", headers
                )
                socket.close()
                return
            }

            val input = BufferedInputStream(socket.getInputStream())
            val output = BufferedOutputStream(socket.getOutputStream())

            // 3. Parse HTTP request line & headers
            val requestLine = readLine(input) ?: run { socket.close(); return }
            val parts = requestLine.split(" ")
            if (parts.size < 2) { socket.close(); return }

            val method = parts[0].uppercase()
            val fullPath = parts[1]
            val path = fullPath.substringBefore("?")
            val queryString = if (fullPath.contains("?")) fullPath.substringAfter("?") else ""

            val headers = mutableMapOf<String, String>()
            while (true) {
                val line = readLine(input) ?: break
                if (line.isEmpty()) break
                val colonIdx = line.indexOf(':')
                if (colonIdx > 0) {
                    val key = line.substring(0, colonIdx).trim().lowercase()
                    val value = line.substring(colonIdx + 1).trim()
                    headers[key] = value
                }
            }

            // 4. Handle HTTP helper server requests (port 8080)
            if (!isHttps) {
                if (path == "/cert") {
                    handleCertDownload(output)
                } else {
                    handleHttpHelperLanding(output, headers["host"] ?: clientIp)
                }
                output.flush()
                socket.close()
                return
            }

            // 5. Protected endpoint authentication
            val isPublic = path == "/health" ||
                    path == "/api/pair" ||
                    path == "/api/pair/refresh" ||
                    path == "/api/pair/info" ||
                    path == "/cert"

            if (!isPublic && path.startsWith("/api/")) {
                val authHeader = headers["authorization"]
                if (authHeader == null || !authHeader.startsWith("Bearer ")) {
                    sendResponse(
                        output, 401, "Unauthorized", "application/json",
                        "{\"error\":\"Unauthorized: missing bearer token\"}"
                    )
                    output.flush()
                    socket.close()
                    return
                }
                val token = authHeader.removePrefix("Bearer ").trim()
                val session = pairManager.validateSession(token)
                if (session == null) {
                    sendResponse(
                        output, 401, "Unauthorized", "application/json",
                        "{\"error\":\"Unauthorized: invalid or expired session\"}"
                    )
                    output.flush()
                    socket.close()
                    return
                }
            }

            // 6. Route HTTPS request
            when {
                path == "/health" && method == "GET" -> handleHealth(output)
                path == "/api/pair/info" && method == "GET" -> handlePairInfo(output)
                path == "/api/pair" && method == "POST" -> handlePair(input, output, headers, clientIp)
                path == "/api/pair/refresh" && method == "POST" -> handleRefresh(input, output, headers, clientIp)
                path == "/api/revoke" && method == "POST" -> handleRevoke(output, clientIp)
                path == "/api/files" && method == "GET" -> handleFilesList(output)
                path == "/api/clipboard" && method == "GET" -> handleGetClipboard(output)
                path == "/api/clipboard" && method == "POST" -> handlePostClipboard(input, output, headers)
                path == "/api/clipboard" && method == "DELETE" -> handleDeleteClipboard(output)
                path == "/api/upload" && method == "POST" -> handleUpload(input, output, headers)
                path == "/api/download" && method == "GET" -> handleDownload(output, queryString, headers)
                path == "/cert" && method == "GET" -> handleCertDownload(output)
                else -> sendResponse(output, 404, "Not Found", "application/json", "{\"error\":\"Not found\"}")
            }

            output.flush()
        } catch (e: Exception) {
            Log.e(tag, "[PANIC_RECOVERED] Exception from ${maskIp(clientIp)}: ${e.message}")
            try {
                val out = socket.getOutputStream()
                sendResponse(out, 500, "Internal Server Error", "application/json", "{\"error\":\"Internal Server Error\"}")
                out.flush()
            } catch (_: Exception) {}
        } finally {
            try { socket.close() } catch (_: Exception) {}
        }
    }

    private fun handleHealth(out: OutputStream) {
        val json = JSONObject().apply {
            put("status", "ok")
            put("service", "HushDrop")
            put("version", "0.3.1")
            put("timestamp", System.currentTimeMillis() / 1000L)
        }
        sendResponse(out, 200, "OK", "application/json", json.toString())
    }

    private fun handlePairInfo(out: OutputStream) {
        val (_, token, expiresAt) = pairManager.getActivePairingDetails()
        val remaining = maxOf(0L, (expiresAt - System.currentTimeMillis()) / 1000L).toInt()

        val json = JSONObject().apply {
            put("fingerprint", tlsBundle.fingerprintSha256)
            put("token", token)
            put("expiresIn", remaining)
        }
        sendResponse(out, 200, "OK", "application/json", json.toString())
    }

    private fun handlePair(input: InputStream, out: OutputStream, headers: Map<String, String>, clientIp: String) {
        val banSec = pairManager.checkIpBan(clientIp)
        if (banSec != null && banSec > 0) {
            val json = JSONObject().apply {
                put("error", "IP banned due to repeated failed attempts, try again in $banSec seconds")
            }
            sendResponse(out, 403, "Forbidden", "application/json", json.toString())
            return
        }

        val bodyStr = readBodyString(input, headers["content-length"]?.toIntOrNull() ?: 0)
        val bodyJson = try { JSONObject(bodyStr) } catch (_: Exception) { JSONObject() }

        val pin = bodyJson.optString("pin", "")
        val token = bodyJson.optString("token", "")
        val clientPubKey: String? = if (bodyJson.has("clientPubKey")) bodyJson.getString("clientPubKey") else null

        try {
            val result = pairManager.verifyPairing(
                clientIp, pin, token, clientPubKey, tlsBundle.fingerprintSha256
            )
            Log.i(tag, "[PAIR_SUCCESS] Paired device from ${maskIp(clientIp)}")

            val resp = JSONObject().apply {
                put("accessToken", result.accessToken)
                put("refreshToken", result.refreshToken)
                put("accessExpiresIn", result.accessExpiresIn)
                put("refreshExpiresIn", result.refreshExpiresIn)
                put("fingerprint", result.fingerprint)
                result.serverPubKey?.let { put("serverPubKey", it) }
            }
            sendResponse(out, 200, "OK", "application/json", resp.toString())
        } catch (e: Exception) {
            Log.w(tag, "[PAIR_FAILED] Failed PIN pairing attempt from ${maskIp(clientIp)}: ${e.message}")
            val resp = JSONObject().apply {
                put("error", e.message ?: "Invalid PIN or token")
            }
            sendResponse(out, 403, "Forbidden", "application/json", resp.toString())
        }
    }

    private fun handleRefresh(input: InputStream, out: OutputStream, headers: Map<String, String>, clientIp: String) {
        val bodyStr = readBodyString(input, headers["content-length"]?.toIntOrNull() ?: 0)
        val bodyJson = try { JSONObject(bodyStr) } catch (_: Exception) { JSONObject() }
        val refreshToken = bodyJson.optString("refreshToken", "")

        try {
            val result = pairManager.refreshSession(refreshToken, clientIp)
            val resp = JSONObject().apply {
                put("accessToken", result.accessToken)
                put("refreshToken", result.refreshToken)
                put("accessExpiresIn", result.accessExpiresIn)
                put("refreshExpiresIn", result.refreshExpiresIn)
            }
            sendResponse(out, 200, "OK", "application/json", resp.toString())
        } catch (e: Exception) {
            val resp = JSONObject().apply { put("error", e.message ?: "Session expired") }
            sendResponse(out, 401, "Unauthorized", "application/json", resp.toString())
        }
    }

    private fun handleRevoke(out: OutputStream, clientIp: String) {
        Log.i(tag, "[REVOKE_ALL] 'Forget Everything' triggered from ${maskIp(clientIp)}")
        pairManager.revokeAll()
        transferManager.clearAll()

        synchronized(this) {
            clipboardBytes?.fill(0)
            clipboardBytes = null
            clipboardBurnAfterRead = false
            clipboardUpdatedAt = 0L
        }

        val json = JSONObject().apply {
            put("message", "All sessions revoked, keys zeroized, and files cleared")
        }
        sendResponse(out, 200, "OK", "application/json", json.toString())
    }

    private fun handleFilesList(out: OutputStream) {
        val files = transferManager.listFiles()
        val jsonArray = JSONArray()
        for (f in files) {
            jsonArray.put(JSONObject().apply {
                put("id", f.id)
                put("originalName", f.originalName)
                put("cleanName", f.cleanName)
                put("size", f.size)
                put("mimeType", f.mimeType)
                put("totalChunks", f.totalChunks)
                put("uploadedChunks", f.uploadedChunks)
                put("sha256", f.sha256)
                put("isDangerous", f.isDangerous)
                put("isCompleted", f.isCompleted)
                put("createdAt", f.createdAt)
            })
        }
        val resp = JSONObject().apply { put("files", jsonArray) }
        sendResponse(out, 200, "OK", "application/json", resp.toString())
    }

    private fun handleGetClipboard(out: OutputStream) {
        synchronized(this) {
            val bytes = clipboardBytes
            if (bytes == null || bytes.isEmpty()) {
                val resp = JSONObject().apply {
                    put("text", "")
                    put("isEmpty", true)
                    put("burnAfterRead", false)
                    put("updatedAt", 0)
                }
                sendResponse(out, 200, "OK", "application/json", resp.toString())
                return
            }

            val text = String(bytes, StandardCharsets.UTF_8)
            val burn = clipboardBurnAfterRead
            val updated = clipboardUpdatedAt

            if (burn) {
                bytes.fill(0)
                clipboardBytes = null
                clipboardBurnAfterRead = false
                clipboardUpdatedAt = 0L
            }

            val resp = JSONObject().apply {
                put("text", text)
                put("burnAfterRead", burn)
                put("updatedAt", updated)
                put("isEmpty", false)
            }
            sendResponse(out, 200, "OK", "application/json", resp.toString())
        }
    }

    private fun handlePostClipboard(input: InputStream, out: OutputStream, headers: Map<String, String>) {
        val contentLength = headers["content-length"]?.toIntOrNull() ?: 0
        if (contentLength > 1024 * 1024) {
            val resp = JSONObject().apply { put("error", "Clipboard text exceeds 1MB limit") }
            sendResponse(out, 413, "Payload Too Large", "application/json", resp.toString())
            return
        }

        val bodyStr = readBodyString(input, contentLength)
        val bodyJson = try { JSONObject(bodyStr) } catch (_: Exception) { JSONObject() }
        val text = bodyJson.optString("text", "")
        val burn = bodyJson.optBoolean("burnAfterRead", false)

        val rawBytes = text.toByteArray(StandardCharsets.UTF_8)
        if (rawBytes.size > 1024 * 1024) {
            val resp = JSONObject().apply { put("error", "Clipboard text exceeds 1MB limit") }
            sendResponse(out, 413, "Payload Too Large", "application/json", resp.toString())
            return
        }

        synchronized(this) {
            clipboardBytes?.fill(0)
            clipboardBytes = rawBytes
            clipboardBurnAfterRead = burn
            clipboardUpdatedAt = System.currentTimeMillis() / 1000L
        }

        val resp = JSONObject().apply {
            put("success", true)
            put("updatedAt", clipboardUpdatedAt)
        }
        sendResponse(out, 200, "OK", "application/json", resp.toString())
    }

    private fun handleDeleteClipboard(out: OutputStream) {
        synchronized(this) {
            clipboardBytes?.fill(0)
            clipboardBytes = null
            clipboardBurnAfterRead = false
            clipboardUpdatedAt = 0L
        }
        val resp = JSONObject().apply {
            put("message", "Clipboard cleared and zeroized")
        }
        sendResponse(out, 200, "OK", "application/json", resp.toString())
    }

    private fun handleUpload(input: InputStream, out: OutputStream, headers: Map<String, String>) {
        val fileId = headers["x-file-id"]
        val chunkIdxStr = headers["x-chunk-index"]
        val totalChunksStr = headers["x-total-chunks"]
        val fileName = headers["x-file-name"] ?: "file.bin"
        val fileSizeStr = headers["x-file-size"] ?: "0"

        if (fileId.isNullOrEmpty() || chunkIdxStr.isNullOrEmpty() || totalChunksStr.isNullOrEmpty()) {
            val resp = JSONObject().apply {
                put("error", "Missing required chunk headers (X-File-Id, X-Chunk-Index, X-Total-Chunks)")
            }
            sendResponse(out, 400, "Bad Request", "application/json", resp.toString())
            return
        }

        val chunkIndex = chunkIdxStr.toIntOrNull() ?: -1
        val totalChunks = totalChunksStr.toIntOrNull() ?: -1
        val fileSize = fileSizeStr.toLongOrNull() ?: 0L
        val contentLength = headers["content-length"]?.toLongOrNull() ?: 0L

        if (chunkIndex < 0 || totalChunks <= 0) {
            val resp = JSONObject().apply { put("error", "Invalid chunk index or total chunks") }
            sendResponse(out, 400, "Bad Request", "application/json", resp.toString())
            return
        }

        if (contentLength > transferManager.maxChunkSize) {
            val resp = JSONObject().apply { put("error", "Chunk size $contentLength exceeds 4MB limit") }
            sendResponse(out, 413, "Payload Too Large", "application/json", resp.toString())
            return
        }

        if (fileSize > transferManager.maxFileSize) {
            val resp = JSONObject().apply { put("error", "Total file size $fileSize exceeds 5GB limit") }
            sendResponse(out, 413, "Payload Too Large", "application/json", resp.toString())
            return
        }

        // Init upload on chunk 0
        if (chunkIndex == 0) {
            try {
                transferManager.initUpload(fileId, fileName, fileSize, totalChunks)
                Log.i(tag, "[UPLOAD_START] Transfer ID ${fileId.take(8)} for file ${maskFilename(fileName)}")
            } catch (e: Exception) {
                val resp = JSONObject().apply { put("error", "Init upload failed: ${e.message}") }
                sendResponse(out, 400, "Bad Request", "application/json", resp.toString())
                return
            }
        }

        // Stream chunk into transfer manager
        try {
            val meta = transferManager.writeChunk(fileId, chunkIndex, contentLength, input)
            if (meta.isCompleted) {
                Log.i(tag, "[UPLOAD_DONE] Completed file ${maskFilename(meta.cleanName)}, SHA256: ${meta.sha256.take(12)}...")
            }

            val fileJson = JSONObject().apply {
                put("id", meta.id)
                put("originalName", meta.originalName)
                put("cleanName", meta.cleanName)
                put("size", meta.size)
                put("mimeType", meta.mimeType)
                put("totalChunks", meta.totalChunks)
                put("uploadedChunks", meta.uploadedChunks)
                put("sha256", meta.sha256)
                put("isDangerous", meta.isDangerous)
                put("isCompleted", meta.isCompleted)
                put("createdAt", meta.createdAt)
            }

            val resp = JSONObject().apply {
                put("success", true)
                put("chunkIndex", chunkIndex)
                put("uploadedChunks", meta.uploadedChunks)
                put("totalChunks", meta.totalChunks)
                put("isCompleted", meta.isCompleted)
                put("file", fileJson)
            }
            sendResponse(out, 200, "OK", "application/json", resp.toString())
        } catch (e: Exception) {
            Log.e(tag, "[UPLOAD_CHUNK_ERR] fileId $fileId chunk $chunkIndex: ${e.message}")
            val resp = JSONObject().apply { put("error", "Failed to write chunk or finalize file") }
            sendResponse(out, 500, "Internal Server Error", "application/json", resp.toString())
        }
    }

    private fun handleDownload(out: OutputStream, queryString: String, headers: Map<String, String>) {
        val params = parseQueryParams(queryString)
        val fileId = params["id"]

        if (fileId.isNullOrEmpty()) {
            val resp = JSONObject().apply { put("error", "Missing file id parameter") }
            sendResponse(out, 400, "Bad Request", "application/json", resp.toString())
            return
        }

        val meta = transferManager.getFile(fileId)
        if (meta == null || meta.filePath == null || !meta.filePath!!.exists()) {
            val resp = JSONObject().apply { put("error", "File not found or transfer incomplete") }
            sendResponse(out, 404, "Not Found", "application/json", resp.toString())
            return
        }

        val file = meta.filePath!!
        val fileLength = file.length()
        val rangeHeader = headers["range"]

        val safeFilename = meta.cleanName.replace("\"", "_")
        val securityWarningHeader = if (meta.isDangerous) "X-Security-Warning: Potentially executable file\r\n" else ""

        if (rangeHeader != null && rangeHeader.startsWith("bytes=")) {
            // Range request: 206 Partial Content
            val rangeVal = rangeHeader.removePrefix("bytes=").trim()
            val dashIdx = rangeVal.indexOf('-')
            var start = 0L
            var end = fileLength - 1L

            if (dashIdx != -1) {
                val startPart = rangeVal.substring(0, dashIdx).trim()
                val endPart = rangeVal.substring(dashIdx + 1).trim()
                if (startPart.isNotEmpty()) start = startPart.toLongOrNull() ?: 0L
                if (endPart.isNotEmpty()) end = endPart.toLongOrNull() ?: (fileLength - 1L)
            }

            start = maxOf(0L, start)
            end = minOf(fileLength - 1L, end)
            val rangeLength = end - start + 1L

            val responseHeaders = "HTTP/1.1 206 Partial Content\r\n" +
                    "Content-Type: ${meta.mimeType}\r\n" +
                    "Content-Length: $rangeLength\r\n" +
                    "Content-Range: bytes $start-$end/$fileLength\r\n" +
                    "Content-Disposition: attachment; filename=\"$safeFilename\"\r\n" +
                    "Accept-Ranges: bytes\r\n" +
                    "X-Content-Type-Options: nosniff\r\n" +
                    securityWarningHeader +
                    "Connection: close\r\n\r\n"

            out.write(responseHeaders.toByteArray(StandardCharsets.UTF_8))

            RandomAccessFile(file, "r").use { raf ->
                raf.seek(start)
                val buffer = ByteArray(64 * 1024)
                var remaining = rangeLength
                while (remaining > 0) {
                    val toRead = minOf(buffer.size.toLong(), remaining).toInt()
                    val read = raf.read(buffer, 0, toRead)
                    if (read == -1) break
                    out.write(buffer, 0, read)
                    remaining -= read
                }
            }
        } else {
            // Full download: 200 OK
            val responseHeaders = "HTTP/1.1 200 OK\r\n" +
                    "Content-Type: ${meta.mimeType}\r\n" +
                    "Content-Length: $fileLength\r\n" +
                    "Content-Disposition: attachment; filename=\"$safeFilename\"\r\n" +
                    "Accept-Ranges: bytes\r\n" +
                    "X-Content-Type-Options: nosniff\r\n" +
                    securityWarningHeader +
                    "Connection: close\r\n\r\n"

            out.write(responseHeaders.toByteArray(StandardCharsets.UTF_8))

            FileInputStream(file).use { fis ->
                val buffer = ByteArray(64 * 1024)
                var read: Int
                while (fis.read(buffer).also { read = it } != -1) {
                    out.write(buffer, 0, read)
                }
            }
        }
    }

    private fun handleCertDownload(out: OutputStream) {
        val certBytes = tlsBundle.certificatePem.toByteArray(StandardCharsets.UTF_8)
        val responseHeaders = "HTTP/1.1 200 OK\r\n" +
                "Content-Type: application/x-x509-ca-cert\r\n" +
                "Content-Disposition: attachment; filename=\"hushdrop.crt\"\r\n" +
                "Content-Length: ${certBytes.size}\r\n" +
                "Connection: close\r\n\r\n"

        out.write(responseHeaders.toByteArray(StandardCharsets.UTF_8))
        out.write(certBytes)
    }

    private fun handleHttpHelperLanding(out: OutputStream, host: String) {
        val cleanHost = host.substringBefore(":")
        val httpsUrl = "https://$cleanHost:$httpsPort/"

        val html = """
            <!DOCTYPE html>
            <html lang="ru">
            <head>
            <meta charset="utf-8">
            <meta name="viewport" content="width=device-width, initial-scale=1.0">
            <title>HushDrop Phone Host — Сертификат и вход</title>
            <style>
            body { background: #0b0f19; color: #e2e8f0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 20px; box-sizing: border-box; }
            .card { background: #161e2e; border: 1px solid #334155; border-radius: 16px; padding: 28px; max-width: 480px; width: 100%; box-shadow: 0 10px 25px rgba(0,0,0,0.5); }
            h1 { font-size: 20px; margin-top: 0; color: #38bdf8; display: flex; align-items: center; gap: 8px; }
            p { font-size: 14px; line-height: 1.6; color: #94a3b8; }
            .fp-box { background: #0f172a; border: 1px solid #1e293b; border-radius: 8px; padding: 12px; font-family: monospace; font-size: 11px; word-break: break-all; color: #22c55e; margin: 16px 0; }
            .btn { display: block; text-align: center; padding: 12px 18px; border-radius: 10px; font-weight: 600; font-size: 14px; text-decoration: none; margin-bottom: 12px; transition: opacity 0.2s; }
            .btn-primary { background: #0284c7; color: #ffffff; }
            .btn-secondary { background: #334155; color: #f1f5f9; }
            .btn:hover { opacity: 0.9; }
            .note { font-size: 12px; color: #64748b; margin-top: 16px; }
            </style>
            </head>
            <body>
            <div class="card">
              <h1>📱 HushDrop Mobile Host</h1>
              <p>Телефон работает в режиме хоста по <b>HTTPS (TLS 1.3)</b> с локальным самоподписанным сертификатом.</p>
              <div class="fp-box">SHA-256 Fingerprint:<br>${tlsBundle.fingerprintSha256}</div>
              <a class="btn btn-primary" href="$httpsUrl">Перейти в HushDrop Web App (HTTPS)</a>
              <a class="btn btn-secondary" href="/cert">Скачать сертификат (hushdrop.crt)</a>
              <p class="note"><b>Подсказка:</b> Если браузер показывает предупреждение о сертификате, нажмите «Дополнительно» (Advanced) &rarr; «Перейти на сайт» (Proceed). Отпечаток сертификата гарантирует отсутствие перехвата в локальной сети.</p>
            </div>
            </body>
            </html>
        """.trimIndent()

        val bytes = html.toByteArray(StandardCharsets.UTF_8)
        val responseHeaders = "HTTP/1.1 200 OK\r\n" +
                "Content-Type: text/html; charset=utf-8\r\n" +
                "Content-Length: ${bytes.size}\r\n" +
                "Connection: close\r\n\r\n"

        out.write(responseHeaders.toByteArray(StandardCharsets.UTF_8))
        out.write(bytes)
    }

    private fun sendResponse(
        out: OutputStream,
        statusCode: Int,
        statusText: String,
        contentType: String,
        body: String,
        extraHeaders: List<String> = emptyList()
    ) {
        val bodyBytes = body.toByteArray(StandardCharsets.UTF_8)
        val sb = StringBuilder()
        sb.append("HTTP/1.1 $statusCode $statusText\r\n")
        sb.append("Content-Type: $contentType\r\n")
        sb.append("Content-Length: ${bodyBytes.size}\r\n")
        sb.append("X-Content-Type-Options: nosniff\r\n")
        for (h in extraHeaders) {
            sb.append("$h\r\n")
        }
        sb.append("Connection: close\r\n\r\n")

        out.write(sb.toString().toByteArray(StandardCharsets.UTF_8))
        out.write(bodyBytes)
    }

    private fun readLine(input: InputStream): String? {
        val baos = ByteArrayOutputStream()
        var prev = -1
        while (true) {
            val b = input.read()
            if (b == -1) {
                if (baos.size() == 0) return null
                break
            }
            if (prev == '\r'.code && b == '\n'.code) {
                val bytes = baos.toByteArray()
                return String(bytes, 0, bytes.size - 1, StandardCharsets.UTF_8)
            }
            baos.write(b)
            prev = b
        }
        return baos.toString("UTF-8")
    }

    private fun readBodyString(input: InputStream, length: Int): String {
        if (length <= 0) return ""
        val bytes = ByteArray(length)
        var totalRead = 0
        while (totalRead < length) {
            val r = input.read(bytes, totalRead, length - totalRead)
            if (r == -1) break
            totalRead += r
        }
        return String(bytes, 0, totalRead, StandardCharsets.UTF_8)
    }

    private fun parseQueryParams(query: String): Map<String, String> {
        val map = mutableMapOf<String, String>()
        if (query.isEmpty()) return map
        for (pair in query.split("&")) {
            val idx = pair.indexOf('=')
            if (idx > 0) {
                val key = URLDecoder.decode(pair.substring(0, idx), "UTF-8")
                val value = URLDecoder.decode(pair.substring(idx + 1), "UTF-8")
                map[key] = value
            }
        }
        return map
    }
}
