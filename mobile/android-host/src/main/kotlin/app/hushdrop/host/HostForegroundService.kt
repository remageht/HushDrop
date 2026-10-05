package app.hushdrop.host

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.net.wifi.WifiManager
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import android.util.Log
import androidx.core.app.NotificationCompat

class HostForegroundService : Service() {

    private val tag = "HostForegroundService"
    private var wakeLock: PowerManager.WakeLock? = null
    private var wifiLock: WifiManager.WifiLock? = null

    companion object {
        const val CHANNEL_ID = "hushdrop_host_channel"
        const val NOTIFICATION_ID = 8443

        const val ACTION_START = "app.hushdrop.host.START"
        const val ACTION_STOP = "app.hushdrop.host.STOP"

        var activeServer: HushDropServer? = null
            private set
        var activeTlsBundle: HostTlsBundle? = null
            private set
        var nsdHelper: NsdHelper? = null
            private set
        var hostIp: String = "127.0.0.1"
            private set
        var isRunning: Boolean = false
            private set

        fun start(context: Context) {
            val intent = Intent(context, HostForegroundService::class.java).apply {
                action = ACTION_START
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        fun stop(context: Context) {
            val intent = Intent(context, HostForegroundService::class.java).apply {
                action = ACTION_STOP
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> startHostServer()
            ACTION_STOP -> stopHostServer()
        }
        return START_NOT_STICKY
    }

    private fun startHostServer() {
        if (isRunning) return

        try {
            // 1. Acquire power locks
            val powerManager = getSystemService(Context.POWER_SERVICE) as? PowerManager
            wakeLock = powerManager?.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "HushDrop:WakeLock")?.apply {
                acquire(2 * 60 * 60 * 1000L) // 2 hours max safe timeout
            }

            val wifiManager = applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
            wifiLock = wifiManager?.createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "HushDrop:WifiLock")?.apply {
                acquire()
            }

            // 2. Discover Best IP
            val localIps = CertificateGenerator.getLocalIpAddresses()
            hostIp = localIps.firstOrNull { it.startsWith("192.168.") || it.startsWith("10.") } ?: "127.0.0.1"

            // 3. Generate TLS certificate bundle
            val tls = CertificateGenerator.generateSelfSignedBundle(localIps)
            activeTlsBundle = tls

            // 4. Instantiate managers
            val pairMgr = PairManager()
            val transferMgr = TransferManager(applicationContext)

            val server = HushDropServer(
                context = applicationContext,
                tlsBundle = tls,
                pairManager = pairMgr,
                transferManager = transferMgr,
                httpsPort = 8443,
                httpPort = 8080
            )
            server.start()
            activeServer = server

            // 5. Register NSD
            val nsd = NsdHelper(applicationContext)
            nsd.registerService(8443, tls.fingerprintSha256)
            nsdHelper = nsd

            val (pin, _, _) = pairMgr.getActivePairingDetails()

            // 6. Show Foreground Notification
            val notification = buildNotification(hostIp, pin)
            startForeground(NOTIFICATION_ID, notification)

            isRunning = true
            val maskedIp = HushDropServer.maskIp(hostIp)
            Log.i(tag, "HostForegroundService running on port 8443 (host: $maskedIp)")
        } catch (e: Exception) {
            Log.e(tag, "Failed to start HostForegroundService: ${e.message}", e)
            stopSelf()
        }
    }

    private fun stopHostServer() {
        try {
            nsdHelper?.unregisterService()
            nsdHelper = null

            activeServer?.stop()
            activeServer = null
            activeTlsBundle = null

            if (wakeLock?.isHeld == true) wakeLock?.release()
            wakeLock = null

            if (wifiLock?.isHeld == true) wifiLock?.release()
            wifiLock = null

            isRunning = false
            stopForeground(true)
            stopSelf()
            Log.i(tag, "HostForegroundService stopped successfully")
        } catch (e: Exception) {
            Log.e(tag, "Error stopping host service: ${e.message}")
        }
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                "HushDrop Host Service",
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = "Фоновый сервер HushDrop для прямой передачи файлов"
                setShowBadge(false)
            }
            val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            manager.createNotificationChannel(channel)
        }
    }

    private fun buildNotification(ip: String, pin: String): Notification {
        val launchIntent = packageManager.getLaunchIntentForPackage(packageName)
        val pendingIntent = if (launchIntent != null) {
            PendingIntent.getActivity(
                this, 0, launchIntent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
            )
        } else null

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("HushDrop Host активен")
            .setContentText("https://$ip:8443 • PIN: $pin")
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
    }

    override fun onDestroy() {
        stopHostServer()
        super.onDestroy()
    }
}
