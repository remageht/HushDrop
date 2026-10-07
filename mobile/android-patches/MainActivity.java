package app.hushdrop;

import android.content.Intent;
import android.os.Bundle;
import com.getcapacitor.BridgeActivity;
import app.hushdrop.host.HushDropHostPlugin;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        registerPlugin(HushDropHostPlugin.class);
        super.onCreate(savedInstanceState);
        HushDropHostPlugin.handleSendIntent(this, getIntent());
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        HushDropHostPlugin.handleSendIntent(this, intent);
    }
}
