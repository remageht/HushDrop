package app.hushdrop.host

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Build
import android.util.Log

class NsdHelper(private val context: Context) {
    private val tag = "NsdHelper"
    private val nsdManager = context.getSystemService(Context.NSD_SERVICE) as? NsdManager
    private var registrationListener: NsdManager.RegistrationListener? = null
    private var isRegistered = false

    fun registerService(port: Int, fingerprint: String, serviceName: String = "HushDrop-MobileHost") {
        if (nsdManager == null || isRegistered) return

        try {
            val serviceInfo = NsdServiceInfo().apply {
                this.serviceName = serviceName
                this.serviceType = "_hushdrop._tcp."
                this.port = port
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.LOLLIPOP) {
                    setAttribute("fp", fingerprint)
                    setAttribute("v", "0.1.0")
                }
            }

            registrationListener = object : NsdManager.RegistrationListener {
                override fun onServiceRegistered(serviceInfo: NsdServiceInfo) {
                    isRegistered = true
                    Log.i(tag, "NSD Service registered: ${serviceInfo.serviceName} on port $port")
                }

                override fun onRegistrationFailed(serviceInfo: NsdServiceInfo, errorCode: Int) {
                    isRegistered = false
                    Log.w(tag, "NSD Service registration failed, error code: $errorCode")
                }

                override fun onServiceUnregistered(serviceInfo: NsdServiceInfo) {
                    isRegistered = false
                    Log.i(tag, "NSD Service unregistered successfully")
                }

                override fun onUnregistrationFailed(serviceInfo: NsdServiceInfo, errorCode: Int) {
                    Log.w(tag, "NSD Service unregistration failed, error code: $errorCode")
                }
            }

            nsdManager.registerService(serviceInfo, NsdManager.PROTOCOL_DNS_SD, registrationListener)
        } catch (e: Exception) {
            Log.e(tag, "Error registering NSD service: ${e.message}")
        }
    }

    fun unregisterService() {
        if (!isRegistered || nsdManager == null || registrationListener == null) return
        try {
            nsdManager.unregisterService(registrationListener)
            isRegistered = false
        } catch (e: Exception) {
            Log.w(tag, "Failed to unregister NSD service: ${e.message}")
        }
    }
}
