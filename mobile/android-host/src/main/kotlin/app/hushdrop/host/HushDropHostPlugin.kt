package app.hushdrop.host

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.OpenableColumns
import android.util.Base64
import android.util.Log
import android.webkit.MimeTypeMap
import com.getcapacitor.JSArray
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import java.io.File
import java.util.Collections

@CapacitorPlugin(name = "HushDropHost")
class HushDropHostPlugin : Plugin() {

    override fun load() {
        super.load()
        activeInstance = this
    }

    override fun handleOnNewIntent(intent: Intent) {
        super.handleOnNewIntent(intent)
        handleSendIntent(context, intent)
    }

    override fun handleOnDestroy() {
        if (activeInstance == this) {
            activeInstance = null
        }
        super.handleOnDestroy()
    }

    @PluginMethod
    fun startHost(call: PluginCall) {
        try {
            HostForegroundService.start(context)

            // Allow quick spin-up
            Thread.sleep(150)

            val ret = buildStatusObject()
            call.resolve(ret)
        } catch (e: Exception) {
            call.reject("Failed to start host server: ${e.message}", e)
        }
    }

    @PluginMethod
    fun stopHost(call: PluginCall) {
        try {
            HostForegroundService.stop(context)
            val ret = JSObject().apply {
                put("isRunning", false)
            }
            call.resolve(ret)
        } catch (e: Exception) {
            call.reject("Failed to stop host server: ${e.message}", e)
        }
    }

    @PluginMethod
    fun getHostStatus(call: PluginCall) {
        try {
            val ret = buildStatusObject()
            call.resolve(ret)
        } catch (e: Exception) {
            call.reject("Failed to get host status: ${e.message}", e)
        }
    }

    @PluginMethod
    fun getNetworkInfo(call: PluginCall) {
        try {
            val ips = CertificateGenerator.getLocalIpAddresses()
            val ret = JSObject().apply {
                put("ips", com.getcapacitor.JSArray(ips))
                put("selectedIp", HostForegroundService.hostIp)
            }
            call.resolve(ret)
        } catch (e: Exception) {
            call.reject("Failed to get network info: ${e.message}", e)
        }
    }

    @PluginMethod
    fun addHostedFile(call: PluginCall) {
        val path = call.getString("path")
        val name = call.getString("name") ?: "file.bin"
        val mimeType = call.getString("mimeType") ?: "application/octet-stream"

        if (path.isNullOrEmpty()) {
            call.reject("Path is required")
            return
        }

        val file = File(path)
        if (!file.exists() || !file.canRead()) {
            call.reject("File does not exist or cannot be read at $path")
            return
        }

        val server = HostForegroundService.activeServer
        if (server == null) {
            call.reject("Host server is not currently running")
            return
        }

        try {
            val meta = server.transferManager.addHostedFile(file, name, mimeType)
            val fileObj = JSObject().apply {
                put("id", meta.id)
                put("originalName", meta.originalName)
                put("cleanName", meta.cleanName)
                put("size", meta.size)
                put("mimeType", meta.mimeType)
                put("sha256", meta.sha256)
                put("isDangerous", meta.isDangerous)
                put("isCompleted", true)
            }
            val ret = JSObject().apply {
                put("success", true)
                put("file", fileObj)
            }
            call.resolve(ret)
        } catch (e: Exception) {
            call.reject("Failed to add hosted file: ${e.message}", e)
        }
    }

    private fun buildStatusObject(): JSObject {
        val isRunning = HostForegroundService.isRunning
        val server = HostForegroundService.activeServer
        val tls = HostForegroundService.activeTlsBundle
        val ip = HostForegroundService.hostIp

        val ret = JSObject()
        ret.put("isRunning", isRunning)
        ret.put("ip", ip)
        ret.put("port", 8443)
        ret.put("httpPort", 8080)

        if (isRunning && server != null && tls != null) {
            val (pin, token, expiresAt) = server.pairManager.getActivePairingDetails()
            val remainingSec = maxOf(0L, (expiresAt - System.currentTimeMillis()) / 1000L).toInt()
            val fp = tls.fingerprintSha256
            val qrUrl = "https://$ip:8443/?token=$token&fp=$fp"

            ret.put("pin", pin)
            ret.put("token", token)
            ret.put("fingerprint", fp)
            ret.put("expiresIn", remainingSec)
            ret.put("activeSessions", server.pairManager.getActiveSessionCount())
            ret.put("filesCount", server.transferManager.listFiles().size)
            ret.put("qrUrl", qrUrl)
        }

        return ret
    }

    @PluginMethod
    fun getSharedFiles(call: PluginCall) {
        val ret = JSObject()
        val filesArr = JSArray()
        synchronized(pendingSharedFiles) {
            for (f in pendingSharedFiles) {
                filesArr.put(f.toJSObject())
            }
            pendingSharedFiles.clear()
        }
        ret.put("files", filesArr)
        call.resolve(ret)
    }

    @PluginMethod
    fun clearSharedFiles(call: PluginCall) {
        synchronized(pendingSharedFiles) {
            pendingSharedFiles.clear()
        }
        try {
            val cacheDir = File(context.cacheDir, "shared_incoming")
            if (cacheDir.exists()) {
                cacheDir.listFiles()?.forEach { it.delete() }
            }
        } catch (_: Exception) {}
        call.resolve()
    }

    @PluginMethod
    fun readSharedFileBase64(call: PluginCall) {
        val path = call.getString("path")
        if (path.isNullOrEmpty()) {
            call.reject("Path is required")
            return
        }
        val file = File(path)
        if (!file.exists() || !file.canRead()) {
            call.reject("File does not exist or cannot be read")
            return
        }
        try {
            val bytes = file.readBytes()
            val base64: String = Base64.encodeToString(bytes, Base64.NO_WRAP)
            val ret = JSObject().apply {
                put("data", base64)
            }
            call.resolve(ret)
        } catch (e: Exception) {
            call.reject("Failed to read file: ${e.message}", e)
        }
    }

    companion object {
        private const val TAG = "HushDropHostPlugin"
        private val pendingSharedFiles = Collections.synchronizedList(mutableListOf<SharedFileInfo>())
        private var activeInstance: HushDropHostPlugin? = null

        @JvmStatic
        fun handleSendIntent(context: Context, intent: Intent?) {
            if (intent == null) return
            val action = intent.action ?: return
            if (Intent.ACTION_SEND != action && Intent.ACTION_SEND_MULTIPLE != action) {
                return
            }

            val uris = mutableListOf<Uri>()

            if (Intent.ACTION_SEND == action) {
                getStreamUri(intent)?.let { uris.add(it) }
            } else if (Intent.ACTION_SEND_MULTIPLE == action) {
                getStreamUriList(intent)?.let { uris.addAll(it) }
            }

            // Fallback to clipData
            if (uris.isEmpty() && intent.clipData != null) {
                val clip = intent.clipData!!
                for (i in 0 until clip.itemCount) {
                    clip.getItemAt(i).uri?.let { uris.add(it) }
                }
            }

            val extraText = intent.getStringExtra(Intent.EXTRA_TEXT)
            val newItems = mutableListOf<SharedFileInfo>()

            for (uri in uris) {
                try {
                    val info = copyUriToCache(context, uri, intent.type)
                    if (info != null) {
                        newItems.add(info)
                    }
                } catch (e: Exception) {
                    Log.e(TAG, "Failed to copy shared stream to cache: ${e.message}")
                }
            }

            // If text was shared with or without files
            if (!extraText.isNullOrBlank() && newItems.isEmpty()) {
                try {
                    val textInfo = saveTextToCache(context, extraText)
                    newItems.add(textInfo)
                } catch (e: Exception) {
                    Log.e(TAG, "Failed to cache shared text: ${e.message}")
                }
            }

            if (newItems.isNotEmpty()) {
                synchronized(pendingSharedFiles) {
                    pendingSharedFiles.addAll(newItems)
                }
                activeInstance?.let { plugin ->
                    val arr = JSArray()
                    for (item in newItems) {
                        arr.put(item.toJSObject())
                    }
                    val data = JSObject().apply {
                        put("files", arr)
                    }
                    plugin.notifyListeners("shareReceived", data)
                }
            }
        }

        private fun getStreamUri(intent: Intent): Uri? {
            return try {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                    intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
                } else {
                    @Suppress("DEPRECATION")
                    intent.getParcelableExtra(Intent.EXTRA_STREAM) as? Uri
                }
            } catch (_: Exception) {
                null
            }
        }

        private fun getStreamUriList(intent: Intent): List<Uri>? {
            return try {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                    intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java)
                } else {
                    @Suppress("DEPRECATION")
                    intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM)
                }
            } catch (_: Exception) {
                null
            }
        }

        private fun sanitizeFileName(raw: String): String {
            var clean = raw.replace("\u0000", "").replace("../", "").replace("..\\", "")
            clean = clean.replace('/', '_').replace('\\', '_')
            clean = clean.trim('.', ' ', '\t', '\n', '\r')
            if (clean.isBlank()) {
                clean = "shared_file"
            }
            return clean
        }

        private fun copyUriToCache(context: Context, uri: Uri, intentType: String?): SharedFileInfo? {
            var displayName: String? = null
            var fileSize: Long = -1

            try {
                context.contentResolver.query(uri, null, null, null, null)?.use { cursor ->
                    val nameIdx = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    val sizeIdx = cursor.getColumnIndex(OpenableColumns.SIZE)
                    if (cursor.moveToFirst()) {
                        if (nameIdx != -1 && !cursor.isNull(nameIdx)) {
                            displayName = cursor.getString(nameIdx)
                        }
                        if (sizeIdx != -1 && !cursor.isNull(sizeIdx)) {
                            fileSize = cursor.getLong(sizeIdx)
                        }
                    }
                }
            } catch (_: Exception) {}

            if (displayName.isNullOrBlank()) {
                displayName = uri.lastPathSegment ?: "shared_file"
            }

            var cleanName = sanitizeFileName(displayName!!)
            val mimeType = context.contentResolver.getType(uri) ?: intentType ?: "application/octet-stream"

            if (!cleanName.contains('.')) {
                val ext = MimeTypeMap.getSingleton().getExtensionFromMimeType(mimeType)
                if (!ext.isNullOrBlank()) {
                    cleanName = "$cleanName.$ext"
                }
            }

            val cacheDir = File(context.cacheDir, "shared_incoming")
            if (!cacheDir.exists()) {
                cacheDir.mkdirs()
            }

            val targetFile = File(cacheDir, "${System.currentTimeMillis()}_$cleanName")
            context.contentResolver.openInputStream(uri)?.use { input ->
                targetFile.outputStream().use { output ->
                    input.copyTo(output, bufferSize = 64 * 1024)
                }
            } ?: return null

            if (fileSize <= 0) {
                fileSize = targetFile.length()
            }

            return SharedFileInfo(
                name = cleanName,
                size = fileSize,
                mime = mimeType,
                cachePath = targetFile.absolutePath
            )
        }

        private fun saveTextToCache(context: Context, text: String): SharedFileInfo {
            val cacheDir = File(context.cacheDir, "shared_incoming")
            if (!cacheDir.exists()) {
                cacheDir.mkdirs()
            }
            val cleanName = "shared_text_${System.currentTimeMillis()}.txt"
            val targetFile = File(cacheDir, cleanName)
            targetFile.writeText(text, Charsets.UTF_8)
            return SharedFileInfo(
                name = cleanName,
                size = targetFile.length(),
                mime = "text/plain",
                cachePath = targetFile.absolutePath,
                text = text
            )
        }
    }
}

data class SharedFileInfo(
    val name: String,
    val size: Long,
    val mime: String,
    val cachePath: String,
    val text: String? = null
) {
    fun toJSObject(): JSObject = JSObject().apply {
        put("name", name)
        put("size", size)
        put("mime", mime)
        put("cachePath", cachePath)
        text?.let { put("text", it) }
    }
}
