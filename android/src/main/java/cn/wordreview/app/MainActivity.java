package cn.wordreview.app;

import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.ClipData;
import android.content.Intent;
import android.content.res.Configuration;
import android.graphics.Color;
import android.graphics.Rect;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.speech.tts.TextToSpeech;
import android.speech.tts.Voice;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.view.WindowInsets;
import android.webkit.CookieManager;
import android.webkit.JavascriptInterface;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.FrameLayout;
import android.widget.TextView;

import org.json.JSONObject;
import org.json.JSONException;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.Proxy;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

public final class MainActivity extends Activity implements LocalEngine.Listener {
    private static final int SYSTEM_DOCUMENT = 310;
    private static final int HTML_DOCUMENT = 311;
    private static final long MAX_FILE = 128L * 1024 * 1024;
    private final ExecutorService io = Executors.newSingleThreadExecutor();
    private FrameLayout root;
    private TextView status;
    private WebView web;
    private Uri localUrl;
    private String credential;
    private Operation operation;
    private Uri delayedDocument;
    private ValueCallback<Uri[]> htmlFile;
    private TextToSpeech speech;
    private volatile boolean speechReady;
    private boolean visible;
    private boolean pageReady;
    private String pendingResult;
    private boolean darkAppearance;
    private boolean keyboardVisible;

    private static final int LIGHT_CANVAS = Color.rgb(245, 247, 250);
    private static final int DARK_CANVAS = Color.rgb(22, 24, 27);
    private static final int LIGHT_SURFACE = Color.rgb(255, 255, 255);
    private static final int DARK_NAVIGATION = Color.rgb(27, 29, 33);
    private static final int DARK_INK = Color.rgb(238, 241, 245);

    @Override public void onCreate(Bundle saved) {
        super.onCreate(saved);
        root = new FrameLayout(this);
        applyAppearance(startupDark());
        setContentView(root);
        watchKeyboard();
        if (saved != null && saved.getString("documentKind") != null) {
            operation = new Operation(saved.getString("documentKind"), saved.getString("documentBody", ""));
        }
        showStatus("正在打开背词本…");
        LocalEngine.INSTANCE.connect(getApplicationContext(), this);
        initSpeech();
    }

    private void showStatus(String message) {
        if (status == null) {
            status = new TextView(this);
            status.setTextSize(16);
            status.setGravity(Gravity.CENTER);
            status.setPadding(22, 22, 22, 22);
            root.addView(status, new FrameLayout.LayoutParams(-1, -1));
        }
        status.setTextColor(darkAppearance ? DARK_INK : Color.rgb(32, 40, 51));
        status.setText(message);
        status.setVisibility(android.view.View.VISIBLE);
        status.bringToFront();
    }

    @Override public void ready(String launchUrl) {
        if (isDestroyed()) return;
        Uri entry = Uri.parse(launchUrl);
        pageReady = false;
        localUrl = entry;
        String token = entry.getLastPathSegment();
        credential = "wordreview_" + token.substring(0, 10) + "=" + token;
        if (web != null) {
            root.removeView(web);
            web.destroy();
        }
        try {
            web = new WebView(this);
        } catch (RuntimeException error) {
            showStatus("系统 WebView 无法启动。请更新或启用 Android System WebView，再打开背词本。");
            return;
        }
        root.addView(web, 0, new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
        web.setBackgroundColor(darkAppearance ? DARK_CANVAS : LIGHT_CANVAS);
        WebSettings settings = web.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        settings.setAllowFileAccess(false);
        // HTML uploads are chosen through the system picker. Direct content navigation is blocked.
        settings.setAllowContentAccess(true);
        settings.setAllowFileAccessFromFileURLs(false);
        settings.setAllowUniversalAccessFromFileURLs(false);
        settings.setJavaScriptCanOpenWindowsAutomatically(false);
        settings.setSupportMultipleWindows(false);
        settings.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
        if (Build.VERSION.SDK_INT >= 33) settings.setAlgorithmicDarkeningAllowed(false);
        else if (Build.VERSION.SDK_INT >= 29) settings.setForceDark(WebSettings.FORCE_DARK_OFF);
        applyFontScale();
        CookieManager.getInstance().setAcceptCookie(true);
        CookieManager.getInstance().setAcceptThirdPartyCookies(web, false);
        web.addJavascriptInterface(new Bridge(), "WordReviewFiles");
        web.setWebViewClient(new WebViewClient() {
            @Override public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
                Uri uri = request.getUrl();
                if (local(uri)) return false;
                if (request.isForMainFrame() && "https".equals(uri.getScheme())) openExternal(uri);
                return true;
            }

            @Override public void onPageFinished(WebView view, String url) {
                if (local(Uri.parse(url)) && "/".equals(Uri.parse(url).getPath())) {
                    pageReady = true;
                    notifyKeyboard();
                    if (status != null) status.setVisibility(android.view.View.GONE);
                    if (pendingResult != null) {
                        runJS(pendingResult);
                        pendingResult = null;
                    }
                }
            }
        });
        web.setWebChromeClient(new WebChromeClient() {
            @Override public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> result, FileChooserParams params) {
                if (htmlFile != null) htmlFile.onReceiveValue(null);
                htmlFile = result;
                Intent picker = document(Intent.ACTION_OPEN_DOCUMENT, "*/*");
                try {
                    startActivityForResult(picker, HTML_DOCUMENT);
                } catch (ActivityNotFoundException e) {
                    htmlFile.onReceiveValue(null);
                    htmlFile = null;
                }
                return true;
            }
        });
        web.loadUrl(launchUrl);
        if (delayedDocument != null && operation != null) {
            Uri uri = delayedDocument;
            delayedDocument = null;
            startDocumentWork(uri);
        }
    }

    @Override public void ended(String error) {
        localUrl = null;
        credential = null;
        if (isDestroyed()) return;
        if (error == null) {
            finish();
            return;
        }
        showStatus(error + "\n点击重试");
        status.setOnClickListener(v -> {
            status.setOnClickListener(null);
            showStatus("正在重新打开背词本…");
            LocalEngine.INSTANCE.connect(getApplicationContext(), this);
        });
    }

    private boolean local(Uri uri) {
        Uri base = localUrl;
        return base != null && "http".equals(uri.getScheme()) && "127.0.0.1".equals(uri.getHost()) &&
            uri.getPort() == base.getPort() && uri.getUserInfo() == null;
    }

    private void openExternal(Uri uri) {
        if (!"https".equals(uri.getScheme()) || uri.getHost() == null) return;
        try {
            startActivity(new Intent(Intent.ACTION_VIEW, uri));
        } catch (ActivityNotFoundException ignored) {}
    }

    private void applyFontScale() {
        if (web == null) return;
        float scale = getResources().getConfiguration().fontScale;
        web.getSettings().setTextZoom(Math.round(scale * 100));
    }

    private void watchKeyboard() {
        if (Build.VERSION.SDK_INT >= 30) {
            root.setOnApplyWindowInsetsListener((view, insets) -> {
                keyboardChanged(insets.isVisible(WindowInsets.Type.ime()));
                return insets;
            });
        } else {
            final Rect frame = new Rect();
            root.getViewTreeObserver().addOnGlobalLayoutListener(() -> {
                root.getWindowVisibleDisplayFrame(frame);
                float density = getResources().getDisplayMetrics().density;
                keyboardChanged(root.getRootView().getHeight() - frame.bottom > 100 * density);
            });
        }
    }

    private void keyboardChanged(boolean shown) {
        if (keyboardVisible == shown) return;
        keyboardVisible = shown;
        if (pageReady) notifyKeyboard();
    }

    private void notifyKeyboard() {
        runJS("window.wordReviewKeyboardChanged?.(" + keyboardVisible + ")");
    }

    @Override public void onConfigurationChanged(Configuration value) {
        super.onConfigurationChanged(value);
        applyFontScale();
        if (pageReady) runJS("window.wordReviewSystemThemeChanged?.()");
        else applyAppearance(startupDark());
    }

    private boolean isSystemDark() {
        return (getResources().getConfiguration().uiMode & Configuration.UI_MODE_NIGHT_MASK)
            == Configuration.UI_MODE_NIGHT_YES;
    }

    private boolean startupDark() {
        File preferences = new File(getFilesDir(), "StudyData/appearance.json");
        try {
            if (preferences.length() > 0 && preferences.length() <= 1024) {
                String text = new String(Files.readAllBytes(preferences.toPath()), StandardCharsets.UTF_8);
                String theme = new JSONObject(text).optString("theme");
                if ("dark".equals(theme)) return true;
                if ("light".equals(theme)) return false;
            }
        } catch (IOException | JSONException ignored) { }
        return isSystemDark();
    }

    private void applyAppearance(boolean dark) {
        darkAppearance = dark;
        int canvas = dark ? DARK_CANVAS : LIGHT_CANVAS;
        root.setBackgroundColor(canvas);
        if (web != null) web.setBackgroundColor(canvas);
        if (status != null) status.setTextColor(dark ? DARK_INK : Color.rgb(32, 40, 51));
        Window window = getWindow();
        window.setStatusBarColor(canvas);
        window.setNavigationBarColor(dark ? DARK_NAVIGATION : LIGHT_SURFACE);
        View decor = window.getDecorView();
        int lightBars = View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR | View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR;
        int flags = decor.getSystemUiVisibility() & ~lightBars;
        decor.setSystemUiVisibility(dark ? flags : flags | lightBars);
    }

    private void runJS(String javascript) {
        if (web != null && !isDestroyed()) web.evaluateJavascript(javascript, null);
    }

    private void initSpeech() {
        speech = new TextToSpeech(this, result -> {
            if (result == TextToSpeech.SUCCESS && speech != null && !isDestroyed()) {
                Set<Voice> voices = speech.getVoices();
                if (voices != null) {
                    for (Voice voice : voices) {
                        Set<String> features = voice.getFeatures();
                        if ("en".equals(voice.getLocale().getLanguage()) && !voice.isNetworkConnectionRequired() &&
                                (features == null || !features.contains(TextToSpeech.Engine.KEY_FEATURE_NOT_INSTALLED))) {
                            speechReady = speech.setVoice(voice) == TextToSpeech.SUCCESS;
                            if (speechReady) break;
                        }
                    }
                }
            }
            runJS("window.wordReviewSpeechChanged?.()");
        });
    }

    @Override protected void onResume() {
        super.onResume();
        visible = true;
        runJS("window.wordReviewSystemThemeChanged?.()");
        runJS("window.wordReviewResume?.()");
    }

    @Override protected void onPause() {
        visible = false;
        runJS("window.wordReviewFlushNote?.()");
        super.onPause();
    }

    @Override public void onBackPressed() {
        if (web == null || localUrl == null) {
            super.onBackPressed();
            return;
        }
        web.evaluateJavascript("window.wordReviewBack?.() || false", value -> {
            if (!"true".equals(value) && !isDestroyed()) {
                runJS("window.wordReviewFlushNote?.()");
                moveTaskToBack(true);
            }
        });
    }

    @Override protected void onDestroy() {
        LocalEngine.INSTANCE.disconnect(this);
        if (htmlFile != null) htmlFile.onReceiveValue(null);
        if (speech != null) {
            speech.stop();
            speech.shutdown();
        }
        if (web != null) {
            web.removeJavascriptInterface("WordReviewFiles");
            root.removeView(web);
            web.destroy();
        }
        // Finish selected document transfers already in progress.
        io.shutdown();
        super.onDestroy();
    }

    @Override protected void onSaveInstanceState(Bundle out) {
        super.onSaveInstanceState(out);
        if (operation != null) {
            out.putString("documentKind", operation.kind);
            out.putString("documentBody", operation.body);
        }
    }

    private static Intent document(String action, String type) {
        return new Intent(action).addCategory(Intent.CATEGORY_OPENABLE).setType(type);
    }

    private void pick(String kind, String body) {
        if (!visible || localUrl == null || operation != null) return;
        if ("recovery".equals(kind)) {
            File latest = latestRecovery();
            if (latest == null) {
                sendResult(kind, null, "还没有覆盖前自动备份。");
                return;
            }
            body = latest.getName();
        }
        operation = new Operation(kind, body);
        Intent selector;
        if ("import".equals(kind)) {
            selector = document(Intent.ACTION_OPEN_DOCUMENT, "*/*");
        } else {
            boolean csv = "csv".equals(kind);
            selector = document(Intent.ACTION_CREATE_DOCUMENT, csv ? "text/csv" : "application/json");
            String day = new java.text.SimpleDateFormat("yyyy-MM-dd", java.util.Locale.ROOT).format(new java.util.Date());
            String base = csv ? "WordReview-words-" : ("backup".equals(kind) ? "WordReview-backup-" : "WordReview-Learning-");
            selector.putExtra(Intent.EXTRA_TITLE, "recovery".equals(kind) ? body : base + day + (csv ? ".csv" : ".json"));
        }
        try {
            startActivityForResult(selector, SYSTEM_DOCUMENT);
        } catch (ActivityNotFoundException error) {
            operation = null;
            sendResult(kind, null, "系统文件选择器不可用。");
        }
    }

    @Override protected void onActivityResult(int request, int result, Intent data) {
        super.onActivityResult(request, result, data);
        if (request == HTML_DOCUMENT) {
            ValueCallback<Uri[]> target = htmlFile;
            htmlFile = null;
            if (target == null) return;
            if (result != RESULT_OK || data == null) {
                target.onReceiveValue(null);
                return;
            }
            Uri uri = data.getData();
            ClipData clip = data.getClipData();
            if (uri == null && clip != null && clip.getItemCount() > 0) uri = clip.getItemAt(0).getUri();
            target.onReceiveValue(uri == null ? null : new Uri[]{uri});
            return;
        }
        if (request != SYSTEM_DOCUMENT || operation == null) return;
        Uri uri = data == null ? null : data.getData();
        if (result != RESULT_OK || uri == null) {
            String kind = operation.kind;
            operation = null;
            sendResult(kind, "{\"cancelled\":true}", null);
            return;
        }
        if (localUrl == null) {
            delayedDocument = uri;
        } else {
            startDocumentWork(uri);
        }
    }

    private void startDocumentWork(Uri uri) {
        Operation chosen = operation;
        Uri base = localUrl;
        String cookie = credential;
        io.execute(() -> {
            String json = null;
            String error = null;
            try {
                json = processDocument(chosen, uri, base, cookie);
            } catch (IOException | RuntimeException ex) {
                error = ex.getMessage();
                if (error == null || error.isEmpty()) error = "文件操作未完成。请重试。";
            }
            final String finalJson = json;
            final String finalError = error;
            runOnUiThread(() -> {
                operation = null;
                sendResult(chosen.kind, finalJson, finalError);
            });
        });
    }

    private String processDocument(Operation op, Uri uri, Uri base, String cookie) throws IOException {
        if ("recovery".equals(op.kind)) {
            if (!op.body.matches("before-transfer-[0-9TZ.]+\\.json")) throw new IOException("备份文件名无效。");
            File file = new File(recoveryDirectory(), op.body);
            try (InputStream response = new FileInputStream(file)) {
                writeDocument(uri, response);
            }
            return "{\"ok\":true}";
        }
        String endpoint;
        switch (op.kind) {
        case "import": endpoint = "/api/transfer/preview"; break;
        case "export": endpoint = "/api/transfer/export"; break;
        case "backup": endpoint = "/api/backup"; break;
        case "csv": endpoint = "/api/export"; break;
        default: throw new IOException("文件操作不受支持。");
        }
        String origin = "http://127.0.0.1:" + base.getPort();
        HttpURLConnection connection = (HttpURLConnection) new URL(origin + endpoint).openConnection(Proxy.NO_PROXY);
        connection.setConnectTimeout(10000);
        connection.setReadTimeout(50000);
        connection.setInstanceFollowRedirects(false);
        connection.setRequestProperty("Cookie", cookie);
        try {
            if ("import".equals(op.kind) || "csv".equals(op.kind)) {
                connection.setRequestMethod("POST");
                connection.setRequestProperty("Content-Type", "application/json");
                connection.setRequestProperty("Origin", origin);
                connection.setDoOutput(true);
                connection.setChunkedStreamingMode(65536);
                try (OutputStream destination = connection.getOutputStream()) {
                    if ("import".equals(op.kind)) {
                        try (InputStream source = getContentResolver().openInputStream(uri)) {
                            if (source == null) throw new IOException("无法读取选择的学习数据文件。");
                            copy(source, destination, MAX_FILE);
                        }
                    } else {
                        destination.write(op.body.getBytes(StandardCharsets.UTF_8));
                    }
                }
            }
            int statusCode = connection.getResponseCode();
            if (statusCode != 200) {
                try (InputStream response = connection.getErrorStream()) {
                    String message = response == null ? "" : text(response);
                    try {
                        throw new IOException(new JSONObject(message).optString("error", "文件操作失败。"));
                    } catch (org.json.JSONException invalid) {
                        throw new IOException("文件操作失败（" + statusCode + "）。");
                    }
                }
            }
            try (InputStream response = connection.getInputStream()) {
                if ("import".equals(op.kind)) return text(response);
                writeDocument(uri, response);
                return "{\"ok\":true}";
            }
        } finally {
            connection.disconnect();
        }
    }

    private File recoveryDirectory() {
        return new File(getFilesDir(), "StudyData/backups");
    }

    private File latestRecovery() {
        File[] files = recoveryDirectory().listFiles((dir, name) -> name.matches("before-transfer-[0-9TZ.]+\\.json"));
        File latest = null;
        if (files != null) {
            for (File file : files) {
                if (file.isFile() && (latest == null || file.getName().compareTo(latest.getName()) > 0)) latest = file;
            }
        }
        return latest;
    }

    private void writeDocument(Uri uri, InputStream response) throws IOException {
        try (OutputStream destination = getContentResolver().openOutputStream(uri, "wt")) {
            if (destination == null) throw new IOException("无法写入选择的保存位置。");
            copy(response, destination, MAX_FILE);
            destination.flush();
        }
    }

    private static String text(InputStream input) throws IOException {
        ByteArrayOutputStream result = new ByteArrayOutputStream();
        copy(input, result, 65536);
        return new String(result.toByteArray(), StandardCharsets.UTF_8);
    }

    private static void copy(InputStream input, OutputStream output, long limit) throws IOException {
        byte[] buffer = new byte[32768];
        long size = 0;
        for (int count; (count = input.read(buffer)) != -1; ) {
            size += count;
            if (size > limit) throw new IOException("文件大小超出限制。");
            output.write(buffer, 0, count);
        }
    }

    private void sendResult(String kind, String data, String error) {
        String javascript = "window.wordReviewFileResult?.(" + JSONObject.quote(kind) + "," + JSONObject.quote(data) + "," +
            JSONObject.quote(error) + ")";
        if (pageReady) runJS(javascript);
        else pendingResult = javascript;
    }

    private static final class Operation {
        final String kind;
        final String body;
        Operation(String kind, String body) {
            this.kind = kind;
            this.body = body;
        }
    }

    private final class Bridge {
        @JavascriptInterface public boolean isSystemDark() { return MainActivity.this.isSystemDark(); }

        @JavascriptInterface public void applyAppearance(boolean dark) {
            runOnUiThread(() -> { if (!isDestroyed()) MainActivity.this.applyAppearance(dark); });
        }

        @JavascriptInterface public void pickFile(String kind, String body) {
            if (!"import".equals(kind) && !"export".equals(kind) && !"backup".equals(kind) &&
                    !"csv".equals(kind) && !"recovery".equals(kind)) return;
            if (body == null || body.length() > 1000000) return;
            runOnUiThread(() -> pick(kind, body));
        }

        @JavascriptInterface public boolean canSpeak() { return speechReady; }

        @JavascriptInterface public void speak(String word) {
            if (word == null || word.length() > 200) return;
            runOnUiThread(() -> {
                if (speechReady && speech != null) speech.speak(word, TextToSpeech.QUEUE_FLUSH, null, "word");
            });
        }
    }
}
