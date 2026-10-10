#!/usr/bin/env node

/**
 * HushDrop Android Customization Script
 *
 * Applies required patches to generated Capacitor Android platform:
 * 1. Copies network_security_config.xml and MainActivity.java from android-patches/
 * 2. Patches settings.gradle to include ':android-host'
 * 3. Patches app/build.gradle with dynamic version functions, packaging exclusions, and ':android-host' dependency
 * 4. Patches AndroidManifest.xml with networkSecurityConfig, singleTask, deep-linking, share intents, and service isolation
 *
 * Fully idempotent: running multiple times produces identical results with zero diff.
 */

const fs = require('fs');
const path = require('path');

const scriptDir = __dirname;
const mobileDir = path.resolve(scriptDir, '..');
const androidDir = path.join(mobileDir, 'android');
const patchesDir = path.join(mobileDir, 'android-patches');

if (!fs.existsSync(androidDir)) {
  console.error(`[apply-android-custom] Error: Directory not found: ${androidDir}`);
  console.error('[apply-android-custom] Please run "npx cap add android" first.');
  process.exit(1);
}

console.log('[apply-android-custom] Applying HushDrop Android customizations...');

// 1. Copy network_security_config.xml
const resXmlDir = path.join(androidDir, 'app/src/main/res/xml');
if (!fs.existsSync(resXmlDir)) {
  fs.mkdirSync(resXmlDir, { recursive: true });
}
const netSecSource = path.join(patchesDir, 'network_security_config.xml');
const netSecDest = path.join(resXmlDir, 'network_security_config.xml');
if (fs.existsSync(netSecSource)) {
  fs.copyFileSync(netSecSource, netSecDest);
  console.log('  -> Copied network_security_config.xml');
} else {
  console.warn(`  -> Warning: ${netSecSource} not found`);
}

// 2. Copy MainActivity.java
const mainActivitySource = path.join(patchesDir, 'MainActivity.java');
const mainActivityDest = path.join(androidDir, 'app/src/main/java/app/hushdrop/MainActivity.java');
const mainActivityDir = path.dirname(mainActivityDest);
if (!fs.existsSync(mainActivityDir)) {
  fs.mkdirSync(mainActivityDir, { recursive: true });
}
if (fs.existsSync(mainActivitySource)) {
  fs.copyFileSync(mainActivitySource, mainActivityDest);
  console.log('  -> Copied canonical MainActivity.java');
} else {
  console.warn(`  -> Warning: ${mainActivitySource} not found`);
}

// 3. Patch settings.gradle
const settingsGradlePath = path.join(androidDir, 'settings.gradle');
if (fs.existsSync(settingsGradlePath)) {
  let settings = fs.readFileSync(settingsGradlePath, 'utf8');
  if (!settings.includes("':android-host'")) {
    const androidHostInclude = "\ninclude ':android-host'\nproject(':android-host').projectDir = new File('../android-host')\n";
    if (settings.includes("apply from: 'capacitor.settings.gradle'")) {
      settings = settings.replace("apply from: 'capacitor.settings.gradle'", `${androidHostInclude}\napply from: 'capacitor.settings.gradle'`);
    } else {
      settings += androidHostInclude;
    }
    fs.writeFileSync(settingsGradlePath, settings, 'utf8');
    console.log('  -> Patched settings.gradle (included :android-host)');
  } else {
    console.log('  -> settings.gradle already includes :android-host (skipping)');
  }
}

// 3b. Patch root build.gradle for Kotlin gradle plugin
const rootBuildGradlePath = path.join(androidDir, 'build.gradle');
if (fs.existsSync(rootBuildGradlePath)) {
  let rootGradle = fs.readFileSync(rootBuildGradlePath, 'utf8');
  if (!rootGradle.includes('kotlin-gradle-plugin')) {
    if (rootGradle.includes("classpath 'com.android.tools.build:gradle:8.2.1'")) {
      rootGradle = rootGradle.replace(
        "classpath 'com.android.tools.build:gradle:8.2.1'",
        "classpath 'com.android.tools.build:gradle:8.2.1'\n        classpath 'org.jetbrains.kotlin:kotlin-gradle-plugin:1.9.22'"
      );
      fs.writeFileSync(rootBuildGradlePath, rootGradle, 'utf8');
      console.log('  -> Patched root build.gradle (added kotlin-gradle-plugin)');
    }
  }
}

// 4. Patch app/build.gradle
const appBuildGradlePath = path.join(androidDir, 'app/build.gradle');
if (fs.existsSync(appBuildGradlePath)) {
  let gradle = fs.readFileSync(appBuildGradlePath, 'utf8');

  // 4a. Version calculation functions
  if (!gradle.includes('def getAppVersionName()')) {
    const versionFunctions = `
def getAppVersionName() {
    def pkg = file('../../package.json')
    if (pkg.exists()) {
        def json = new groovy.json.JsonSlurper().parseText(pkg.text)
        return json.version ?: "0.3.5"
    }
    return "0.3.5"
}

def getAppVersionCode() {
    def pkg = file('../../package.json')
    if (pkg.exists()) {
        def json = new groovy.json.JsonSlurper().parseText(pkg.text)
        if (json.versionCode) return json.versionCode.toInteger()
        def v = json.version ?: "0.3.5"
        def parts = v.tokenize('.')
        if (parts.size() >= 3) {
            try {
                return parts[0].toInteger() * 1000 + parts[1].toInteger() * 10 + parts[2].toInteger()
            } catch (Exception ignored) {}
        }
    }
    return 35
}
`;
    if (gradle.includes("apply plugin: 'com.android.application'")) {
      gradle = gradle.replace("apply plugin: 'com.android.application'", `apply plugin: 'com.android.application'\n${versionFunctions}`);
    } else {
      gradle = versionFunctions + '\n' + gradle;
    }
    console.log('  -> Injected getAppVersionName() and getAppVersionCode()');
  }

  // 4b. Update versionCode and versionName in defaultConfig
  if (gradle.includes('versionCode 1') || gradle.match(/versionCode 3\d/)) {
    gradle = gradle.replace(/versionCode \d+/, 'versionCode getAppVersionCode()');
    console.log('  -> Updated versionCode to getAppVersionCode()');
  }
  if (gradle.includes('versionName "1.0"') || gradle.match(/versionName "0\.3\.\d"/)) {
    gradle = gradle.replace(/versionName "[^"]*"/, 'versionName getAppVersionName()');
    console.log('  -> Updated versionName to getAppVersionName()');
  }

  // 4c. Packaging options
  if (!gradle.includes('excludes += [') && !gradle.includes("'META-INF/io.netty.versions.properties'")) {
    const packagingBlock = `
    packaging {
        resources {
            excludes += [
                'META-INF/versions/9/OSGI-INF/MANIFEST.MF',
                'META-INF/versions/9/OSGI-INF/*',
                'META-INF/INDEX.LIST',
                'META-INF/io.netty.versions.properties'
            ]
            pickFirsts += [
                'META-INF/**'
            ]
        }
    }
`;
    // Insert before closing brace of android {
    const androidIdx = gradle.indexOf('android {');
    if (androidIdx !== -1) {
      // Find matching or end of android block
      const lastClosingBrace = gradle.lastIndexOf('\n}');
      const repositoriesIdx = gradle.indexOf('repositories {');
      const dependenciesIdx = gradle.indexOf('dependencies {');
      const insertBefore = (repositoriesIdx !== -1 ? repositoriesIdx : (dependenciesIdx !== -1 ? dependenciesIdx : -1));
      
      if (insertBefore !== -1) {
        // Find last closing brace before insertBefore
        const lastBraceBefore = gradle.lastIndexOf('}', insertBefore);
        if (lastBraceBefore !== -1) {
          gradle = gradle.slice(0, lastBraceBefore) + packagingBlock + gradle.slice(lastBraceBefore);
          console.log('  -> Injected packaging exclusions block');
        }
      }
    }
  }

  // 4d. Dependency on :android-host
  if (!gradle.includes("project(':android-host')")) {
    if (gradle.includes('dependencies {')) {
      gradle = gradle.replace('dependencies {', "dependencies {\n    implementation project(':android-host')");
      console.log('  -> Injected implementation project(\':android-host\')');
    }
  }

  fs.writeFileSync(appBuildGradlePath, gradle, 'utf8');
}

// 5. Patch AndroidManifest.xml
const manifestPath = path.join(androidDir, 'app/src/main/AndroidManifest.xml');
if (fs.existsSync(manifestPath)) {
  let manifest = fs.readFileSync(manifestPath, 'utf8');

  // 5a. networkSecurityConfig & cleartext
  if (!manifest.includes('android:networkSecurityConfig')) {
    manifest = manifest.replace(
      '<application',
      '<application\n        android:networkSecurityConfig="@xml/network_security_config"'
    );
    console.log('  -> Added android:networkSecurityConfig to <application>');
  }
  if (!manifest.includes('android:usesCleartextTraffic')) {
    manifest = manifest.replace(
      '<application',
      '<application\n        android:usesCleartextTraffic="true"'
    );
    console.log('  -> Added android:usesCleartextTraffic to <application>');
  }

  // 5b. MainActivity launchMode
  if (!manifest.includes('android:launchMode="singleTask"')) {
    manifest = manifest.replace(
      'android:name=".MainActivity"',
      'android:name=".MainActivity"\n            android:launchMode="singleTask"'
    );
    console.log('  -> Set android:launchMode="singleTask" on MainActivity');
  }

  // 5c. Intent filters inside MainActivity
  const customSchemeFilter = `
            <!-- Custom Scheme Deep Linking: hushdrop://pair -->
            <intent-filter android:autoVerify="false">
                <action android:name="android.intent.action.VIEW" />
                <category android:name="android.intent.category.DEFAULT" />
                <category android:name="android.intent.category.BROWSABLE" />
                <data android:scheme="hushdrop" android:host="pair" />
            </intent-filter>`;

  const httpsFilter = `
            <!-- Standard HTTPS Deep Linking from QR Code: https://<lan-ip>:8443/?token=... -->
            <intent-filter>
                <action android:name="android.intent.action.VIEW" />
                <category android:name="android.intent.category.DEFAULT" />
                <category android:name="android.intent.category.BROWSABLE" />
                <data android:scheme="https" android:port="8443" />
            </intent-filter>`;

  const sendFilter = `
            <!-- System Share Target (Single File): Send to HushDrop -->
            <intent-filter>
                <action android:name="android.intent.action.SEND" />
                <category android:name="android.intent.category.DEFAULT" />
                <data android:mimeType="*/*" />
            </intent-filter>`;

  const sendMultipleFilter = `
            <!-- System Share Target (Multiple Files): Send to HushDrop -->
            <intent-filter>
                <action android:name="android.intent.action.SEND_MULTIPLE" />
                <category android:name="android.intent.category.DEFAULT" />
                <data android:mimeType="*/*" />
            </intent-filter>`;

  let activityEndIdx = manifest.indexOf('</activity>');
  if (activityEndIdx !== -1) {
    let activityBlock = manifest.slice(0, activityEndIdx);
    let additions = '';

    if (!activityBlock.includes('android:scheme="hushdrop"')) {
      additions += customSchemeFilter;
    }
    if (!activityBlock.includes('android:scheme="https"')) {
      additions += httpsFilter;
    }
    if (!activityBlock.includes('android.intent.action.SEND"')) {
      additions += sendFilter;
    }
    if (!activityBlock.includes('android.intent.action.SEND_MULTIPLE"')) {
      additions += sendMultipleFilter;
    }

    if (additions) {
      manifest = manifest.slice(0, activityEndIdx) + additions + '\n        ' + manifest.slice(activityEndIdx);
      console.log('  -> Injected deep linking and send intent filters into MainActivity');
    }
  }

  // 5d. FileProvider
  if (!manifest.includes('androidx.core.content.FileProvider')) {
    const fileProviderSnippet = `
        <provider
            android:name="androidx.core.content.FileProvider"
            android:authorities="\${applicationId}.fileprovider"
            android:exported="false"
            android:grantUriPermissions="true">
            <meta-data android:name="android.support.FILE_PROVIDER_PATHS" android:resource="@xml/file_paths" />
        </provider>
`;
    const appClosingIdx = manifest.lastIndexOf('</application>');
    if (appClosingIdx !== -1) {
      manifest = manifest.slice(0, appClosingIdx) + fileProviderSnippet + manifest.slice(appClosingIdx);
      console.log('  -> Injected FileProvider into <application>');
    }
  }

  // 5e. HostForegroundService
  if (!manifest.includes('app.hushdrop.host.HostForegroundService')) {
    const serviceSnippet = `
        <service
            android:name="app.hushdrop.host.HostForegroundService"
            android:foregroundServiceType="dataSync"
            android:exported="false" />
`;
    const appClosingIdx = manifest.lastIndexOf('</application>');
    if (appClosingIdx !== -1) {
      manifest = manifest.slice(0, appClosingIdx) + serviceSnippet + manifest.slice(appClosingIdx);
      console.log('  -> Injected HostForegroundService (exported=false) into <application>');
    }
  }

  // 5f. Network permissions before </manifest>
  let perms = '';
  if (!manifest.includes('android.permission.INTERNET')) {
    perms += '    <uses-permission android:name="android.permission.INTERNET" />\n';
  }
  if (!manifest.includes('android.permission.ACCESS_NETWORK_STATE')) {
    perms += '    <uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />\n';
  }
  if (!manifest.includes('android.permission.ACCESS_WIFI_STATE')) {
    perms += '    <uses-permission android:name="android.permission.ACCESS_WIFI_STATE" />\n';
  }
  if (perms) {
    const manifestClosingIdx = manifest.lastIndexOf('</manifest>');
    if (manifestClosingIdx !== -1) {
      manifest = manifest.slice(0, manifestClosingIdx) + perms + manifest.slice(manifestClosingIdx);
      console.log('  -> Injected network permissions into manifest');
    }
  }

  fs.writeFileSync(manifestPath, manifest, 'utf8');
}

console.log('[apply-android-custom] All customizations applied successfully.');
