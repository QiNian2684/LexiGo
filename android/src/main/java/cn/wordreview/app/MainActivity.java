package cn.wordreview.app;

import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.ClipData;
import android.content.Intent;
import android.content.res.Configuration;
import android.database.Cursor;
import android.graphics.Color;
import android.graphics.Rect;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.provider.DocumentsContract;
import android.provider.OpenableColumns;
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
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.Proxy;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.Set;
import java.util.UUID;
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
    private boolean preparingShare;
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
        // Android fits this window to its system bars, including a side bar
        // in landscape. The WebView uses the resulting content bounds.
        if (Build.VERSION.SDK_INT >= 30) getWindow().setDecorFitsSystemWindows(true);
        applyAppearance(startupDark());
        setContentView(root);
        watchKeyboard();
        if (saved != null && saved.getString("documentKind") != null) {
            operation = new Operation(saved.getString("documentKind"), saved.getString("documentBody", ""));
        }
        showStatus("正在打开LexiGo…");
        LocalEngine.INSTANCE.connect(getApplicationContext(), this);
        initSpeech();
        io.execute(this::pruneShares);
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
            showStatus("系统 WebView 无法启动。请更新或启用 Android System WebView，再打开LexiGo。");
            return;
        }
        root.addView(web, 0, new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
        web.setBackgroundColor(darkAppearance ? DARK_CANVAS : LIGHT_CANVAS);
        web.setVerticalScrollBarEnabled(false);
        web.setHorizontalScrollBarEnabled(false);
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
            showStatus("正在重新打开LexiGo…");
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

    private static String studyFilename() {
        String time = new java.text.SimpleDateFormat("yyyy-MM-dd-HH-mm-ss", java.util.Locale.ROOT)
            .format(new java.util.Date());
        return "LexiGo-" + time + StudyShareProvider.FILE_EXTENSION;
    }

    private String documentName(Uri uri) {
        try (Cursor cursor = getContentResolver().query(uri, new String[] {OpenableColumns.DISPLAY_NAME}, null, null, null)) {
            if (cursor != null && cursor.moveToFirst() && !cursor.isNull(0)) return cursor.getString(0);
        } catch (RuntimeException ignored) {}
        return null;
    }

    private static boolean hasStudySuffix(String name) {
        return name != null && name.toLowerCase(java.util.Locale.ROOT).endsWith(StudyShareProvider.FILE_EXTENSION) &&
            !name.matches("(?i).+\\.(?:lexi|learning|json)\\.lexi$");
    }

    private String savedStudyDocument(Uri uri) {
        String name = documentName(uri);
        if (name != null && !hasStudySuffix(name) && DocumentsContract.isDocumentUri(this, uri)) {
            String stem = name.replaceFirst("(?i)(?:\\.(?:lexi|learning|json|bin|octet-stream))+$", "");
            String corrected = stem.trim().isEmpty() ? studyFilename() : stem + StudyShareProvider.FILE_EXTENSION;
            try {
                Uri renamed = DocumentsContract.renameDocument(getContentResolver(), uri, corrected);
                if (renamed != null) name = documentName(renamed);
            } catch (IOException | RuntimeException ignored) {
                // Some providers let apps create and write documents, but do not allow renaming.
            }
        }
        if (hasStudySuffix(name)) return "{\"ok\":true,\"filename\":" + JSONObject.quote(name) + "}";
        String warning = name == null ? "文件已保存，请在文件管理器中核对名称是否以 .lexi 结尾。"
            : "已保存为 " + name + "。此保存位置未保留 .lexi 后缀，LexiGo 仍可按文件内容读取。";
        return "{\"ok\":true,\"warning\":" + JSONObject.quote(warning) + "}";
    }

    private void pruneShares() {
        long expiry = System.currentTimeMillis() - StudyShareProvider.KEEP_MILLIS;
        File[] files = StudyShareProvider.directory(this).listFiles();
        if (files == null) return;
        for (File file : files) {
            String name = file.getName();
            int dot = name.lastIndexOf('.');
            if (dot > 0 && (name.endsWith(StudyShareProvider.FILE_EXTENSION) || name.endsWith(".json")) &&
                    StudyShareProvider.validId(name.substring(0, dot)) && file.isFile() && file.lastModified() < expiry) {
                revokeUriPermission(StudyShareProvider.uri(this, name.substring(0, dot), null), Intent.FLAG_GRANT_READ_URI_PERMISSION);
                file.delete();
            }
        }
    }

    private void shareStudyData() {
        preparingShare = true;
        Uri base = localUrl;
        String cookie = credential;
        io.execute(() -> {
            File directory = StudyShareProvider.directory(this);
            String id = UUID.randomUUID().toString();
            File cache = new File(directory, id + StudyShareProvider.FILE_EXTENSION);
            String filename = studyFilename();
            try {
                pruneShares();
                if (!directory.isDirectory() && !directory.mkdirs()) throw new IOException("无法准备分享文件。");
                try (OutputStream destination = new FileOutputStream(cache)) {
                    processDocument(new Operation("export", ""), null, base, cookie, destination);
                }
                Uri uri = StudyShareProvider.uri(this, id, filename);
                runOnUiThread(() -> {
                    preparingShare = false;
                    if (isDestroyed() || !visible) return;
                    Intent send = new Intent(Intent.ACTION_SEND).setType(StudyShareProvider.FILE_MIME)
                        .putExtra(Intent.EXTRA_STREAM, uri)
                        .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
                    send.setClipData(ClipData.newUri(getContentResolver(), "LexiGo学习数据", uri));
                    try {
                        startActivity(Intent.createChooser(send, "分享学习数据"));
                    } catch (ActivityNotFoundException error) {
                        sendResult("share", null, "当前设备没有可用的分享界面，请使用「保存到本机」。");
                    }
                });
            } catch (IOException | RuntimeException error) {
                cache.delete();
                String reason = error.getMessage();
                runOnUiThread(() -> {
                    preparingShare = false;
                    if (!isDestroyed()) sendResult("share", null, reason == null || reason.isEmpty() ? "无法准备分享文件，请重试。" : reason);
                });
            }
        });
    }

    private void pick(String kind, String body) {
        if (!visible || localUrl == null || operation != null || preparingShare) return;
        if ("share".equals(kind)) {
            shareStudyData();
            return;
        }
        String csvFilename = null;
        if ("csv".equals(kind)) {
            try {
                JSONObject export = new JSONObject(body);
                csvFilename = export.getString("filename");
                if (!csvFilename.matches("WordReview-words-[0-9]{4}-[0-9]{2}-[0-9]{2}\\.csv")) {
                    sendResult(kind, null, "CSV 文件名无效。");
                    return;
                }
                body = export.getJSONObject("selection").toString();
            } catch (org.json.JSONException error) {
                sendResult(kind, null, "CSV 导出范围无效。");
                return;
            }
        } else if ("template".equals(kind)) {
            if (!"common".equals(body) && !"full".equals(body)) return;
            csvFilename = "LexiGo-template-" + body + ".csv";
        }
        operation = new Operation(kind, body);
        Intent selector;
        if ("import".equals(kind)) {
            selector = document(Intent.ACTION_OPEN_DOCUMENT, "*/*");
        } else if (csvFilename != null) {
            selector = document(Intent.ACTION_CREATE_DOCUMENT, "text/csv");
            selector.putExtra(Intent.EXTRA_TITLE, csvFilename);
        } else {
            // Use a neutral MIME type so document providers can keep the requested suffix.
            selector = document(Intent.ACTION_CREATE_DOCUMENT, StudyShareProvider.FILE_MIME);
            selector.putExtra(Intent.EXTRA_TITLE, studyFilename());
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
                json = processDocument(chosen, uri, base, cookie, null);
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

    private String processDocument(Operation op, Uri uri, Uri base, String cookie, OutputStream privateOutput) throws IOException {
        String endpoint;
        switch (op.kind) {
        case "import": endpoint = "/api/transfer/preview"; break;
        case "export": endpoint = "/api/transfer/export"; break;
        case "backup": endpoint = "/api/backup"; break;
        case "recovery": endpoint = "/api/transfer/recovery/download"; break;
        case "csv": endpoint = "/api/export"; break;
        case "template":
            if (!"common".equals(op.body) && !"full".equals(op.body)) throw new IOException("CSV 模板无效。");
            endpoint = "/api/csv-template/" + op.body;
            break;
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
                if (privateOutput == null) writeDocument(uri, response);
                else copy(response, privateOutput, MAX_FILE);
            }
            boolean studyDocument = "export".equals(op.kind) || "backup".equals(op.kind) || "recovery".equals(op.kind);
            return privateOutput == null && studyDocument ? savedStudyDocument(uri) : "{\"ok\":true}";
        } finally {
            connection.disconnect();
        }
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
                    !"csv".equals(kind) && !"template".equals(kind) && !"recovery".equals(kind) && !"share".equals(kind)) return;
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
