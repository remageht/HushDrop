package app.hushdrop.host

import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Locale
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantReadWriteLock
import kotlin.concurrent.read
import kotlin.concurrent.write

data class HostSession(
    val accessToken: String,
    val refreshToken: String,
    val clientIp: String,
    val createdAt: Long,
    val accessExpiresAt: Long,
    val refreshExpiresAt: Long,
    var lastActivity: Long,
    var sharedKey: ByteArray?
)

data class PairResult(
    val accessToken: String,
    val refreshToken: String,
    val accessExpiresIn: Int,
    val refreshExpiresIn: Int,
    val fingerprint: String,
    val serverPubKey: String? = null
)

private data class IpBanTracker(
    var failedAttempts: Int = 0,
    var bannedUntil: Long = 0L
)

class PairManager(
    private val pinTtlMs: Long = 10 * 60 * 1000L,       // 10 minutes
    private val inactivityTtlMs: Long = 15 * 60 * 1000L, // 15 minutes
    private val accessTtlMs: Long = 5 * 60 * 1000L,     // 5 minutes
    private val refreshTtlMs: Long = 60 * 60 * 1000L    // 1 hour
) {
    private val lock = ReentrantReadWriteLock()
    private val secureRandom = SecureRandom()

    private var currentPin: String = ""
    private var pinExpiresAt: Long = 0L
    private var oneTimeToken: String = ""
    private var tokenExpiresAt: Long = 0L

    private val sessions = ConcurrentHashMap<String, HostSession>()      // accessToken -> Session
    private val refreshIndex = ConcurrentHashMap<String, String>()       // refreshToken -> accessToken
    private val ipAttempts = ConcurrentHashMap<String, IpBanTracker>()   // IP -> tracker

    // Rate Limiter: max 60 requests per 60 seconds per IP
    private val ipRateTracker = ConcurrentHashMap<String, MutableList<Long>>()
    private val maxRequestsPerMinute = 60
    private val rateWindowMs = 60 * 1000L

    private val cleanupExecutor = Executors.newSingleThreadScheduledExecutor()

    init {
        generateNewPinAndToken()
        cleanupExecutor.scheduleWithFixedDelay(
            { cleanupExpired() },
            30, 30, TimeUnit.SECONDS
        )
    }

    fun shutdown() {
        cleanupExecutor.shutdownNow()
        revokeAll()
    }

    fun generateNewPinAndToken(): Pair<String, String> = lock.write {
        val pinNum = secureRandom.nextInt(900000) + 100000
        currentPin = String.format(Locale.US, "%06d", pinNum)
        pinExpiresAt = System.currentTimeMillis() + pinTtlMs

        val tokenBytes = ByteArray(24)
        secureRandom.nextBytes(tokenBytes)
        oneTimeToken = tokenBytes.joinToString("") { String.format(Locale.US, "%02x", it) }
        tokenExpiresAt = System.currentTimeMillis() + pinTtlMs

        Pair(currentPin, oneTimeToken)
    }

    fun getActivePairingDetails(): Triple<String, String, Long> = lock.read {
        Triple(currentPin, oneTimeToken, pinExpiresAt)
    }

    // Rate Limiter Check
    fun allowRateLimit(ip: String): Boolean {
        val now = System.currentTimeMillis()
        val cutoff = now - rateWindowMs

        synchronized(ipRateTracker) {
            val list = ipRateTracker.getOrPut(ip) { mutableListOf() }
            list.removeAll { it < cutoff }

            if (list.size >= maxRequestsPerMinute) {
                return false
            }
            list.add(now)
            return true
        }
    }

    // IP Ban Check
    fun checkIpBan(ip: String): Long? {
        val tracker = ipAttempts[ip] ?: return null
        val now = System.currentTimeMillis()
        if (now < tracker.bannedUntil) {
            return (tracker.bannedUntil - now) / 1000L
        }
        return null
    }

    private fun recordFailedAttempt(ip: String) {
        val tracker = ipAttempts.getOrPut(ip) { IpBanTracker() }
        tracker.failedAttempts++
        if (tracker.failedAttempts >= 5) {
            tracker.bannedUntil = System.currentTimeMillis() + (5 * 60 * 1000L) // 5 minutes ban
            tracker.failedAttempts = 0
        }
    }

    private fun clearFailedAttempts(ip: String) {
        ipAttempts.remove(ip)
    }

    fun verifyPairing(
        clientIp: String,
        submittedPin: String,
        submittedToken: String?,
        clientPubKeyHex: String?,
        serverFingerprint: String
    ): PairResult = lock.write {
        // 1. Check ban
        val remainingBanSec = checkIpBan(clientIp)
        if (remainingBanSec != null && remainingBanSec > 0) {
            throw SecurityException("IP banned due to repeated failed attempts, try again in $remainingBanSec seconds")
        }

        val now = System.currentTimeMillis()
        if (now > pinExpiresAt || now > tokenExpiresAt) {
            throw IllegalArgumentException("pairing PIN or QR token has expired")
        }

        // Constant time verification
        val pinMatch = MessageDigest.isEqual(
            currentPin.toByteArray(Charsets.UTF_8),
            submittedPin.toByteArray(Charsets.UTF_8)
        )
        val tokenMatch = submittedToken.isNullOrEmpty() || MessageDigest.isEqual(
            oneTimeToken.toByteArray(Charsets.UTF_8),
            submittedToken.toByteArray(Charsets.UTF_8)
        )

        if (!pinMatch || !tokenMatch) {
            recordFailedAttempt(clientIp)
            throw IllegalArgumentException("invalid PIN or token")
        }

        // Success: clear failed attempts
        clearFailedAttempts(clientIp)

        // Generate tokens
        val accessBytes = ByteArray(32)
        val refreshBytes = ByteArray(32)
        secureRandom.nextBytes(accessBytes)
        secureRandom.nextBytes(refreshBytes)

        val accessToken = accessBytes.joinToString("") { String.format(Locale.US, "%02x", it) }
        val refreshToken = refreshBytes.joinToString("") { String.format(Locale.US, "%02x", it) }

        // Ephemeral shared key in RAM
        val sharedKey = ByteArray(32)
        secureRandom.nextBytes(sharedKey)

        val session = HostSession(
            accessToken = accessToken,
            refreshToken = refreshToken,
            clientIp = clientIp,
            createdAt = now,
            accessExpiresAt = now + accessTtlMs,
            refreshExpiresAt = now + refreshTtlMs,
            lastActivity = now,
            sharedKey = sharedKey
        )

        sessions[accessToken] = session
        refreshIndex[refreshToken] = accessToken

        PairResult(
            accessToken = accessToken,
            refreshToken = refreshToken,
            accessExpiresIn = (accessTtlMs / 1000L).toInt(),
            refreshExpiresIn = (refreshTtlMs / 1000L).toInt(),
            fingerprint = serverFingerprint,
            serverPubKey = null
        )
    }

    fun refreshSession(oldRefreshToken: String, clientIp: String): PairResult = lock.write {
        val oldAccessToken = refreshIndex[oldRefreshToken]
            ?: throw IllegalArgumentException("invalid refresh token")

        val session = sessions[oldAccessToken]
        val now = System.currentTimeMillis()

        if (session == null || now > session.refreshExpiresAt) {
            refreshIndex.remove(oldRefreshToken)
            throw IllegalArgumentException("session expired")
        }

        if (now - session.lastActivity > inactivityTtlMs) {
            sessions.remove(oldAccessToken)
            refreshIndex.remove(oldRefreshToken)
            throw IllegalArgumentException("session expired due to inactivity")
        }

        // Revoke old tokens
        sessions.remove(oldAccessToken)
        refreshIndex.remove(oldRefreshToken)

        // Issue new tokens
        val accessBytes = ByteArray(32)
        val refreshBytes = ByteArray(32)
        secureRandom.nextBytes(accessBytes)
        secureRandom.nextBytes(refreshBytes)

        val newAccessToken = accessBytes.joinToString("") { String.format(Locale.US, "%02x", it) }
        val newRefreshToken = refreshBytes.joinToString("") { String.format(Locale.US, "%02x", it) }

        val newSession = HostSession(
            accessToken = newAccessToken,
            refreshToken = newRefreshToken,
            clientIp = clientIp,
            createdAt = session.createdAt,
            accessExpiresAt = now + accessTtlMs,
            refreshExpiresAt = now + refreshTtlMs,
            lastActivity = now,
            sharedKey = session.sharedKey
        )

        sessions[newAccessToken] = newSession
        refreshIndex[newRefreshToken] = newAccessToken

        PairResult(
            accessToken = newAccessToken,
            refreshToken = newRefreshToken,
            accessExpiresIn = (accessTtlMs / 1000L).toInt(),
            refreshExpiresIn = (refreshTtlMs / 1000L).toInt(),
            fingerprint = ""
        )
    }

    fun validateSession(accessToken: String): HostSession? {
        val session = sessions[accessToken] ?: return null
        val now = System.currentTimeMillis()

        if (now > session.accessExpiresAt) {
            return null
        }
        if (now - session.lastActivity > inactivityTtlMs) {
            lock.write {
                sessions.remove(accessToken)
                session.sharedKey?.let { zeroize(it) }
            }
            return null
        }

        session.lastActivity = now
        return session
    }

    fun revokeAll() = lock.write {
        for ((_, session) in sessions) {
            session.sharedKey?.let { zeroize(it) }
        }
        sessions.clear()
        refreshIndex.clear()
        ipAttempts.clear()
        ipRateTracker.clear()

        // Generate fresh PIN and token
        generateNewPinAndToken()
    }

    fun getActiveSessionCount(): Int = sessions.size

    private fun zeroize(bytes: ByteArray) {
        bytes.fill(0)
    }

    private fun cleanupExpired() = lock.write {
        val now = System.currentTimeMillis()

        val iterator = sessions.entries.iterator()
        while (iterator.hasNext()) {
            val entry = iterator.next()
            val sess = entry.value
            if (now > sess.refreshExpiresAt || (now - sess.lastActivity) > inactivityTtlMs) {
                sess.sharedKey?.let { zeroize(it) }
                refreshIndex.remove(sess.refreshToken)
                iterator.remove()
            }
        }

        // Clean up expired bans
        val banIterator = ipAttempts.entries.iterator()
        while (banIterator.hasNext()) {
            val entry = banIterator.next()
            if (now > entry.value.bannedUntil && entry.value.failedAttempts == 0) {
                banIterator.remove()
            }
        }
    }
}
