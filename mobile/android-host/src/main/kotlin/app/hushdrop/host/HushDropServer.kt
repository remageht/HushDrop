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
import java.net.Inet4Address
import java.net.InetAddress
import java.net.NetworkInterface
import java.net.ServerSocket
import java.net.Socket
import java.net.URL
import java.net.URLDecoder
import java.nio.charset.StandardCharsets
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import javax.net.ssl.HostnameVerifier
import javax.net.ssl.HttpsURLConnection
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLServerSocket
import javax.net.ssl.TrustManager
import javax.net.ssl.X509TrustManager
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit

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
    private val threadPool = Executors.newFixedThreadPool(32)
    private val scanCooldowns = ConcurrentHashMap<String, Long>()
    private val scanInProgress = AtomicBoolean(false)

    private var httpsServerSocket: SSLServerSocket? = null
    private var httpServerSocket: ServerSocket? = null

    private var clipboardBytes: ByteArray? = null
    private var clipboardBurnAfterRead: Boolean = false
    private var clipboardUpdatedAt: Long = 0L

    companion object {
        const val MAX_PAIR_BODY = 64 * 1024
        const val MAX_CLIPBOARD_BODY = 2 * 1024 * 1024
        const val MAX_LINE_LENGTH = 8 * 1024
        const val MAX_HEADER_COUNT = 100

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

        fun isRfc1918(addr: InetAddress): Boolean {
            val ip = addr.hostAddress ?: return false
            if (ip.startsWith("192.168.") || ip.startsWith("10.")) return true
            if (ip.startsWith("172.")) {
                val parts = ip.split(".")
                if (parts.size >= 2) {
                    val second = parts[1].toIntOrNull()
                    if (second != null && second in 16..31) return true
                }
            }
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
                // Disable legacy TLS (1.0/1.1); keep 1.3 with 1.2 as a fallback for older devices
                try {
                    val supported = server.supportedProtocols.toSet()
                    val allowed = listOf("TLSv1.3", "TLSv1.2").filter { supported.contains(it) }
                    if (allowed.isNotEmpty()) server.enabledProtocols = allowed.toTypedArray()
                } catch (_: Exception) {}
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

            val input = BufferedInputStream(socket.getInputStream())
            val output = BufferedOutputStream(socket.getOutputStream())

            // 2. Parse HTTP request line & headers
            val requestLine = readLine(input) ?: run { socket.close(); return }
            val parts = requestLine.split(" ")
            if (parts.size < 2) { socket.close(); return }

            val method = parts[0].uppercase()
            val fullPath = parts[1]
            val path = fullPath.substringBefore("?")
            val queryString = if (fullPath.contains("?")) fullPath.substringAfter("?") else ""

            val headers = mutableMapOf<String, String>()
            var headerCount = 0
            while (headerCount < MAX_HEADER_COUNT) {
                val line = readLine(input) ?: break
                if (line.isEmpty()) break
                headerCount++
                val colonIdx = line.indexOf(':')
                if (colonIdx > 0) {
                    val key = line.substring(0, colonIdx).trim().lowercase()
                    val value = line.substring(colonIdx + 1).trim()
                    headers[key] = value
                }
            }

            // 3. Rate limit check (60/min per IP) - unmetered for GET /, /health, /api/pair/info, static
            val isUnmetered = path == "/" || path == "/health" || path == "/api/pair/info" || !path.startsWith("/api/")
            if (!isUnmetered && !pairManager.allowRateLimit(clientIp)) {
                Log.w(tag, "[RATE_LIMIT] Throttled masked IP ${maskIp(clientIp)} on $path")
                val out = socket.getOutputStream()
                val retryHeaders = listOf("Retry-After: 60")
                sendResponse(
                    out, 429, "Too Many Requests", "application/json",
                    "{\"error\":\"Too many requests. Please slow down.\"}", retryHeaders
                )
                socket.close()
                return
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

            var sessionToken = ""
            if (!isPublic && path.startsWith("/api/")) {
                val authHeader = headers["authorization"]
                // Bearer scheme is case-insensitive per RFC 6750
                if (authHeader == null || !authHeader.startsWith("Bearer ", ignoreCase = true)) {
                    sendResponse(
                        output, 401, "Unauthorized", "application/json",
                        "{\"error\":\"Unauthorized: missing bearer token\"}"
                    )
                    output.flush()
                    socket.close()
                    return
                }
                val token = authHeader.substring(7).trim()
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
                sessionToken = token
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
                path == "/api/nearby/scan" && method == "POST" -> handleNearbyScan(input, output, headers, clientIp, sessionToken)
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
            put("version", "0.3.9")
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

        val contentLength = headers["content-length"]?.toIntOrNull() ?: 0
        if (contentLength > MAX_PAIR_BODY) {
            sendResponse(out, 413, "Payload Too Large", "application/json", "{\"error\":\"Request body too large\"}")
            return
        }

        val bodyStr = readBodyString(input, contentLength)
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
        val contentLength = headers["content-length"]?.toIntOrNull() ?: 0
        if (contentLength > MAX_PAIR_BODY) {
            sendResponse(out, 413, "Payload Too Large", "application/json", "{\"error\":\"Request body too large\"}")
            return
        }

        val bodyStr = readBodyString(input, contentLength)
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
        scanCooldowns.clear()

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

    private fun handleNearbyScan(
        input: InputStream,
        out: OutputStream,
        headers: Map<String, String>,
        clientIp: String,
        sessionToken: String
    ) {
        val now = System.currentTimeMillis()

        // Cooldown check-and-set atomically per session token
        val prev = scanCooldowns.putIfAbsent(sessionToken, now)
        if (prev != null) {
            val elapsed = now - prev
            if (elapsed < 15_000L) {
                val remaining = ((15_000L - elapsed + 999L) / 1000L).toInt().coerceAtLeast(1)
                sendResponse(
                    out, 429, "Too Many Requests", "application/json",
                    "{\"error\":\"Scan rate limit exceeded. Please wait $remaining seconds.\"}",
                    listOf("Retry-After: $remaining")
                )
                return
            }
            scanCooldowns[sessionToken] = now
        }

        // Drop stale cooldown entries so the map cannot grow unbounded
        scanCooldowns.entries.removeIf { now - it.value > 60_000L }

        // Only one scan at a time
        if (!scanInProgress.compareAndSet(false, true)) {
            sendResponse(
                out, 429, "Too Many Requests", "application/json",
                "{\"error\":\"Another scan is already in progress. Please wait a few seconds.\"}",
                listOf("Retry-After: 2")
            )
            return
        }
        try {
            handleNearbyScanLocked(input, out, headers, clientIp)
        } finally {
            scanInProgress.set(false)
        }
    }

    private fun handleNearbyScanLocked(
        input: InputStream,
        out: OutputStream,
        headers: Map<String, String>,
        clientIp: String
    ) {

        val contentLength = headers["content-length"]?.toIntOrNull() ?: 0
        val customPorts = mutableListOf<Int>()
        if (contentLength in 1..4096) {
            try {
                val bodyStr = readBodyString(input, contentLength)
                val json = JSONObject(bodyStr)
                val portsArr = json.optJSONArray("ports")
                if (portsArr != null) {
                    for (i in 0 until minOf(portsArr.length(), 4)) {
                        val p = portsArr.optInt(i)
                        if (p in 1..65535) customPorts.add(p)
                    }
                }
            } catch (_: Exception) {}
        }

        // Discover candidate IPs from primary RFC 1918 interface
        val candidateIps = mutableListOf<String>()
        val localIps = mutableSetOf("127.0.0.1", "::1")

        try {
            val interfaces = NetworkInterface.getNetworkInterfaces()
            while (interfaces.hasMoreElements()) {
                val nif = interfaces.nextElement()
                if (!nif.isUp || nif.isLoopback) continue
                for (ia in nif.interfaceAddresses) {
                    val addr = ia.address
                    if (addr is Inet4Address) {
                        val ipStr = addr.hostAddress ?: continue
                        localIps.add(ipStr)
                        if (candidateIps.isEmpty() && isRfc1918(addr)) {
                            var prefix = ia.networkPrefixLength.toInt()
                            if (prefix < 24) prefix = 24 // clamp to /24

                            val rawBytes = addr.address
                            val ipInt = ((rawBytes[0].toInt() and 0xFF) shl 24) or
                                    ((rawBytes[1].toInt() and 0xFF) shl 16) or
                                    ((rawBytes[2].toInt() and 0xFF) shl 8) or
                                    (rawBytes[3].toInt() and 0xFF)
                            val mask = if (prefix == 0) 0 else (-1 shl (32 - prefix))
                            val netInt = ipInt and mask
                            val hostCount = minOf((1 shl (32 - prefix)) - 2, 256)

                            for (h in 1..hostCount) {
                                val hostInt = netInt + h
                                val hostStr = "${(hostInt ushr 24) and 0xFF}.${(hostInt ushr 16) and 0xFF}.${(hostInt ushr 8) and 0xFF}.${hostInt and 0xFF}"
                                candidateIps.add(hostStr)
                            }
                        }
                    }
                }
            }
        } catch (e: Exception) {
            Log.w(tag, "[NEARBY_SCAN] Error resolving interfaces: ${e.message}")
        }

        val hasCustomPorts = customPorts.isNotEmpty()
        val portsToScan = if (hasCustomPorts) customPorts else listOf(8443)

        data class ProbeTarget(val ip: String, val port: Int)
        val targets = mutableListOf<ProbeTarget>()

        for (ip in candidateIps) {
            val isLocal = localIps.contains(ip)
            for (p in portsToScan) {
                if (isLocal && p == httpsPort) continue // Skip self
                targets.add(ProbeTarget(ip, p))
            }
            if (isLocal && !hasCustomPorts && httpsPort != 8444) {
                targets.add(ProbeTarget(ip, 8444))
            }
        }

        if (!hasCustomPorts && httpsPort == 8443) {
            targets.add(ProbeTarget("127.0.0.1", 8444))
        } else if (!hasCustomPorts && httpsPort == 8444) {
            targets.add(ProbeTarget("127.0.0.1", 8443))
        }

        data class DiscoveredPeer(val ip: String, val port: Int, val version: String, val latencyMs: Long)
        val discovered = mutableListOf<DiscoveredPeer>()

        if (targets.isNotEmpty()) {
            val trustAll = arrayOf<TrustManager>(object : X509TrustManager {
                override fun checkClientTrusted(chain: Array<out java.security.cert.X509Certificate>?, authType: String?) {}
                override fun checkServerTrusted(chain: Array<out java.security.cert.X509Certificate>?, authType: String?) {}
                override fun getAcceptedIssuers(): Array<java.security.cert.X509Certificate> = arrayOf()
            })
            val sslContext = SSLContext.getInstance("TLS").apply {
                init(null, trustAll, java.security.SecureRandom())
            }
            val sslSocketFactory = sslContext.socketFactory
            val hostVerifier = HostnameVerifier { _, _ -> true }

            val semaphore = Semaphore(32)

            runBlocking(Dispatchers.IO) {
                val jobs = targets.map { target ->
                    async {
                        semaphore.withPermit {
                            try {
                                val url = URL("https://${target.ip}:${target.port}/health")
                                val conn = url.openConnection() as HttpsURLConnection
                                conn.sslSocketFactory = sslSocketFactory
                                conn.hostnameVerifier = hostVerifier
                                conn.connectTimeout = 800
                                conn.readTimeout = 800
                                conn.requestMethod = "GET"
                                val start = System.currentTimeMillis()
                                conn.connect()
                                if (conn.responseCode == 200) {
                                    val text = conn.inputStream.bufferedReader().use { it.readText() }
                                    val json = JSONObject(text)
                                    if (json.optString("service") == "HushDrop") {
                                        val latency = maxOf(1L, System.currentTimeMillis() - start)
                                        val ver = json.optString("version", "unknown")
                                        synchronized(discovered) {
                                            discovered.add(DiscoveredPeer(target.ip, target.port, ver, latency))
                                        }
                                    }
                                }
                                conn.disconnect()
                            } catch (_: Exception) {}
                        }
                    }
                }
                jobs.awaitAll()
            }
        }

        discovered.sortBy { it.latencyMs }

        val jsonArray = JSONArray()
        for (node in discovered) {
            val obj = JSONObject().apply {
                put("ip", node.ip)
                put("port", node.port)
                put("version", node.version)
                put("latencyMs", node.latencyMs)
            }
            jsonArray.put(obj)
        }

        Log.i(tag, "[NEARBY_SCAN] Completed for ${maskIp(clientIp)}: ${discovered.size} peers found")
        sendResponse(out, 200, "OK", "application/json", jsonArray.toString())
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
        val contentLength = headers["content-length"]?.toLongOrNull() ?: -1L

        // Chunked transfer encoding is not supported: a declared body size is required
        if (contentLength <= 0) {
            val resp = JSONObject().apply {
                put("error", "Chunk body must not be empty and Content-Length is required")
            }
            sendResponse(out, 411, "Length Required", "application/json", resp.toString())
            return
        }

        if (chunkIndex < 0 || totalChunks <= 0 || chunkIndex >= totalChunks) {
            val resp = JSONObject().apply { put("error", "Invalid chunk index or total chunks") }
            sendResponse(out, 400, "Bad Request", "application/json", resp.toString())
            return
        }

        if (fileSize <= 0) {
            val resp = JSONObject().apply { put("error", "Invalid or missing X-File-Size header") }
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

        // Strip CR/LF/quotes defensively against header injection
        val safeFilename = meta.cleanName
            .replace("\"", "_")
            .replace("\r", "_")
            .replace("\n", "_")
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
                if (startPart.isEmpty()) {
                    // Suffix range: "bytes=-N" means the last N bytes
                    val suffixLen = endPart.toLongOrNull() ?: 0L
                    start = maxOf(0L, fileLength - suffixLen)
                    end = fileLength - 1L
                } else {
                    start = startPart.toLongOrNull() ?: -1L
                    if (endPart.isNotEmpty()) end = endPart.toLongOrNull() ?: (fileLength - 1L)
                }
            }

            start = maxOf(0L, start)
            end = minOf(fileLength - 1L, end)

            if (start > end || start < 0 || fileLength == 0L) {
                val resp = "HTTP/1.1 416 Range Not Satisfiable\r\n" +
                        "Content-Range: bytes */$fileLength\r\n" +
                        "Content-Length: 0\r\n" +
                        "Connection: close\r\n\r\n"
                out.write(resp.toByteArray(StandardCharsets.UTF_8))
                return
            }

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
            .replace("&", "&amp;")
            .replace("<", "&lt;")
            .replace(">", "&gt;")
            .replace("\"", "&quot;")
            .replace("'", "&#39;")
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
            if (baos.size() > MAX_LINE_LENGTH) {
                // Header abuse / malformed request: refuse to buffer further
                return null
            }
            baos.write(b)
            prev = b
        }
        return baos.toString("UTF-8")
    }

    private fun readBodyString(input: InputStream, length: Int): String {
        if (length <= 0) return ""
        // Hard cap on allocation regardless of the declared Content-Length
        val capped = minOf(length, MAX_CLIPBOARD_BODY)
        val bytes = ByteArray(capped)
        var totalRead = 0
        while (totalRead < capped) {
            val r = input.read(bytes, totalRead, capped - totalRead)
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
