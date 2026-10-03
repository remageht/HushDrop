package app.hushdrop.host

import org.bouncycastle.asn1.x500.X500Name
import org.bouncycastle.asn1.x509.BasicConstraints
import org.bouncycastle.asn1.x509.Extension
import org.bouncycastle.asn1.x509.GeneralName
import org.bouncycastle.asn1.x509.GeneralNames
import org.bouncycastle.asn1.x509.KeyUsage
import org.bouncycastle.cert.jcajce.JcaX509CertificateConverter
import org.bouncycastle.cert.jcajce.JcaX509v3CertificateBuilder
import org.bouncycastle.jce.provider.BouncyCastleProvider
import org.bouncycastle.operator.jcajce.JcaContentSignerBuilder
import java.math.BigInteger
import java.net.Inet4Address
import java.net.NetworkInterface
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.Security
import java.security.cert.X509Certificate
import java.security.spec.ECGenParameterSpec
import java.util.Base64
import java.util.Date
import java.util.Locale
import javax.net.ssl.KeyManagerFactory
import javax.net.ssl.SSLContext

data class HostTlsBundle(
    val sslContext: SSLContext,
    val certificate: X509Certificate,
    val keyPair: KeyPair,
    val fingerprintSha256: String,
    val certificatePem: String
)

object CertificateGenerator {

    init {
        if (Security.getProvider("BC") == null) {
            Security.addProvider(BouncyCastleProvider())
        }
    }

    fun generateSelfSignedBundle(customIps: List<String> = emptyList()): HostTlsBundle {
        // 1. Generate ECDSA P-256 Key Pair
        val kpg = KeyPairGenerator.getInstance("EC")
        kpg.initialize(ECGenParameterSpec("secp256r1"))
        val keyPair = kpg.generateKeyPair()

        // 2. Build SAN (Subject Alternative Names)
        val sanList = mutableListOf<GeneralName>()
        sanList.add(GeneralName(GeneralName.dNSName, "localhost"))
        sanList.add(GeneralName(GeneralName.dNSName, "hushdrop.local"))
        sanList.add(GeneralName(GeneralName.iPAddress, "127.0.0.1"))

        // Discover local network interface IPs (Wi-Fi, Hotspot wlan0, ap0, etc.)
        val discoveredIps = getLocalIpAddresses() + customIps
        for (ip in discoveredIps.distinct()) {
            if (ip != "127.0.0.1") {
                try {
                    sanList.add(GeneralName(GeneralName.iPAddress, ip))
                } catch (_: Exception) {}
            }
        }

        val subjectName = X500Name("CN=HushDrop Mobile Host, O=HushDrop, OU=Local Transfer")
        val serial = BigInteger(64, SecureRandom())
        val notBefore = Date(System.currentTimeMillis() - 60_000L) // 1 min buffer
        val notAfter = Date(System.currentTimeMillis() + 365L * 24 * 3600 * 1000L) // 1 year validity

        val certBuilder = JcaX509v3CertificateBuilder(
            subjectName,
            serial,
            notBefore,
            notAfter,
            subjectName,
            keyPair.public
        )

        // Add extensions
        val generalNames = GeneralNames(sanList.toTypedArray())
        certBuilder.addExtension(Extension.subjectAlternativeName, false, generalNames)
        certBuilder.addExtension(Extension.basicConstraints, true, BasicConstraints(false))
        certBuilder.addExtension(
            Extension.keyUsage,
            true,
            KeyUsage(KeyUsage.digitalSignature or KeyUsage.keyEncipherment)
        )

        // Sign certificate
        val signer = JcaContentSignerBuilder("SHA256withECDSA").build(keyPair.private)
        val certHolder = certBuilder.build(signer)
        val certificate: X509Certificate = JcaX509CertificateConverter()
            .setProvider("BC")
            .getCertificate(certHolder)

        // 3. Calculate SHA-256 Fingerprint (AA:BB:CC:...)
        val md = MessageDigest.getInstance("SHA-256")
        val digest = md.digest(certificate.encoded)
        val fingerprint = digest.joinToString(":") { String.format(Locale.US, "%02X", it) }

        // 4. Build PEM representation
        val encoder = Base64.getMimeEncoder(64, "\n".toByteArray())
        val pemBody = encoder.encodeToString(certificate.encoded)
        val certPem = "-----BEGIN CERTIFICATE-----\n$pemBody\n-----END CERTIFICATE-----\n"

        // 5. Build in-memory KeyStore and SSLContext (TLSv1.3 with TLS fallback)
        val keyStorePassword = "hushdrop_ephemeral_pwd".toCharArray()
        val keyStore = KeyStore.getInstance(KeyStore.getDefaultType())
        keyStore.load(null, null)
        keyStore.setKeyEntry("hushdrop", keyPair.private, keyStorePassword, arrayOf(certificate))

        val kmf = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm())
        kmf.init(keyStore, keyStorePassword)

        val sslContext = try {
            SSLContext.getInstance("TLSv1.3").apply {
                init(kmf.keyManagers, null, SecureRandom())
            }
        } catch (_: Exception) {
            SSLContext.getInstance("TLS").apply {
                init(kmf.keyManagers, null, SecureRandom())
            }
        }

        return HostTlsBundle(
            sslContext = sslContext,
            certificate = certificate,
            keyPair = keyPair,
            fingerprintSha256 = fingerprint,
            certificatePem = certPem
        )
    }

    fun getLocalIpAddresses(): List<String> {
        val ips = mutableListOf<String>()
        try {
            val interfaces = NetworkInterface.getNetworkInterfaces() ?: return ips
            while (interfaces.hasMoreElements()) {
                val iface = interfaces.nextElement()
                if (iface.isLoopback || !iface.isUp) continue
                val addresses = iface.inetAddresses
                while (addresses.hasMoreElements()) {
                    val addr = addresses.nextElement()
                    if (addr is Inet4Address && !addr.isLoopbackAddress) {
                        val ipStr = addr.hostAddress
                        if (ipStr != null && (ipStr.startsWith("192.168.") || ipStr.startsWith("10.") || ipStr.startsWith("172."))) {
                            ips.add(ipStr)
                        }
                    }
                }
            }
        } catch (_: Exception) {}
        return ips
    }
}
