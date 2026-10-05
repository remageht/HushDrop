import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'app.hushdrop',
  appName: 'HushDrop',
  webDir: '../frontend/dist',
  backgroundColor: '#0b0f19',
  plugins: {
    SplashScreen: {
      launchShowDuration: 0,
      launchAutoHide: true,
      launchFadeOutDuration: 300,
      backgroundColor: '#0b0f19',
      androidSplashResourceName: 'splash',
      showSpinner: false,
      splashFullScreen: true,
      splashImmersive: true,
    },
  },
  server: {
    androidScheme: 'https',
    cleartext: true, // Required for downloading certificates via local HTTP (:8080/cert)
    allowNavigation: [
      '192.168.*.*',
      '10.*.*.*',
      '172.16.*.*',
      '172.17.*.*',
      '172.18.*.*',
      '172.19.*.*',
      '172.20.*.*',
      '172.21.*.*',
      '172.22.*.*',
      '172.23.*.*',
      '172.24.*.*',
      '172.25.*.*',
      '172.26.*.*',
      '172.27.*.*',
      '172.28.*.*',
      '172.29.*.*',
      '172.30.*.*',
      '172.31.*.*',
      '127.0.0.1',
      'localhost'
    ]
  },
  android: {
    backgroundColor: '#0b0f19',
    allowMixedContent: true,
    captureInput: true,
    webContentsDebuggingEnabled: true
  }
};

export default config;
