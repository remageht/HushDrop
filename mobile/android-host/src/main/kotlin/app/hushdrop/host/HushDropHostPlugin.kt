package app.hushdrop.host

import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin
import java.io.File

@CapacitorPlugin(name = "HushDropHost")
class HushDropHostPlugin : Plugin() {

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
}
