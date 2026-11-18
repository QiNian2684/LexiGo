package cn.wordreview.app;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.content.Context;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;

import java.io.File;
import java.io.FileNotFoundException;
import java.util.List;

/** Read-only provider scoped to generated share files in the private cache. */
public final class StudyShareProvider extends ContentProvider {
    private static final String JSON = "application/json";
    static final long KEEP_MILLIS = 3L * 24 * 60 * 60 * 1000;

    static File directory(Context context) {
        return new File(context.getCacheDir(), "study-share");
    }

    static Uri uri(Context context, String id, String name) {
        return new Uri.Builder().scheme("content").authority(context.getPackageName() + ".share")
            .appendPath("study").appendPath(id).appendPath(name).build();
    }

    static boolean validId(String id) {
        return id.matches("[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}");
    }

    @Override public boolean onCreate() { return true; }

    private File resolve(Uri uri) {
        List<String> parts = uri.getPathSegments();
        if (parts.size() != 3 || !"study".equals(parts.get(0)) || !validId(parts.get(1)) ||
                !parts.get(2).matches("WordReview-Learning-[0-9]{4}-[0-9]{2}-[0-9]{2}\\.json")) {
            return null;
        }
        File file = new File(directory(getContext()), parts.get(1) + ".json");
        return file.isFile() ? file : null;
    }

    @Override public String getType(Uri uri) { return JSON; }

    @Override public ParcelFileDescriptor openFile(Uri uri, String mode) throws FileNotFoundException {
        File file = resolve(uri);
        if (file == null || !"r".equals(mode)) throw new FileNotFoundException("无法读取分享文件。");
        return ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY);
    }

    @Override public Cursor query(Uri uri, String[] projection, String selection, String[] args, String order) {
        File file = resolve(uri);
        if (file == null) return null;
        String[] columns = projection != null ? projection : new String[] {OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE};
        MatrixCursor cursor = new MatrixCursor(columns, 1);
        MatrixCursor.RowBuilder row = cursor.newRow();
        for (String column : columns) {
            Object value = null;
            if (OpenableColumns.DISPLAY_NAME.equals(column)) value = uri.getLastPathSegment();
            if (OpenableColumns.SIZE.equals(column)) value = file.length();
            row.add(value);
        }
        return cursor;
    }

    @Override public Uri insert(Uri uri, ContentValues values) { throw new UnsupportedOperationException(); }
    @Override public int update(Uri uri, ContentValues values, String selection, String[] args) { throw new UnsupportedOperationException(); }
    @Override public int delete(Uri uri, String selection, String[] args) { throw new UnsupportedOperationException(); }
}
