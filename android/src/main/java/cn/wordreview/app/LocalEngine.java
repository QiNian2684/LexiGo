package cn.wordreview.app;

import android.content.Context;
import android.os.Handler;
import android.os.Looper;

import java.io.File;
import java.util.TimeZone;

final class LocalEngine {
    interface Listener {
        void ready(String launchUrl);
        void ended(String error);
    }

    static final LocalEngine INSTANCE = new LocalEngine();
    private final Handler main = new Handler(Looper.getMainLooper());
    private Listener listener;
    private String launchUrl;
    private boolean running;
    private final Object lock = new Object();

    void connect(Context context, Listener owner) {
        final File data = new File(context.getFilesDir(), "StudyData");
        synchronized (lock) {
            listener = owner;
            if (launchUrl != null) {
                owner.ready(launchUrl);
                return;
            }
            if (running) return;
            running = true;
        }
        Thread engine = new Thread(() -> {
            String error;
            try {
                System.loadLibrary("wordreview");
                error = serve(data.getAbsolutePath(), TimeZone.getDefault().getID());
            } catch (LinkageError | RuntimeException e) {
                error = "无法加载本地学习引擎。请重新安装与手机架构匹配的背词本。";
            }
            final String result = error;
            main.post(() -> {
                Listener target;
                synchronized (lock) {
                    launchUrl = null;
                    running = false;
                    target = listener;
                }
                if (target != null) target.ended(result);
            });
        }, "WordReview engine");
        engine.start();
    }

    void disconnect(Listener owner) {
        synchronized (lock) {
            if (listener == owner) listener = null;
        }
    }

    // Called by the shared Go engine on the JNI thread.
    private void onReady(String url) {
        main.post(() -> {
            Listener target;
            synchronized (lock) {
                launchUrl = url;
                target = listener;
            }
            if (target != null) target.ready(url);
        });
    }

    private native String serve(String directory, String zone);
}
