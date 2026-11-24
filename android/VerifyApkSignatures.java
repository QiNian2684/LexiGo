import com.android.apksig.ApkVerifier;
import java.io.File;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.HexFormat;

class VerifyApkSignatures {
    // SHA-256 of the release certificate for cn.wordreview.app.lexigo.android.
    private static final String RELEASE_CERTIFICATE =
        "2684b553b2221ec91c2cde3a3494f029e440f201d4b142963a6d7cac4ab2df95";

    public static void main(String[] args) throws Exception {
        if (args.length < 1 || args.length > 2 || (args.length == 2 && !args[1].equals("--release"))) {
            throw new IllegalArgumentException("Usage: VerifyApkSignatures <apk> [--release]");
        }
        File apk = new File(args[0]);
        File incrementalSignature = new File(args[0] + ".idsig");
        if (!incrementalSignature.isFile() || incrementalSignature.length() == 0) {
            throw new IllegalArgumentException("Missing or empty v4 signature: " + incrementalSignature);
        }
        ApkVerifier.Result result = new ApkVerifier.Builder(apk)
            .setMinCheckedPlatformVersion(23)
            .setV4SignatureFile(incrementalSignature)
            .build()
            .verify();
        if (!result.isVerified()) {
            throw new IllegalArgumentException("APK signature verification failed: " + result.getAllErrors());
        }
        var warnings = new ArrayList<>(result.getWarnings());
        result.getV1SchemeSigners().forEach(signer -> warnings.addAll(signer.getWarnings()));
        result.getV2SchemeSigners().forEach(signer -> warnings.addAll(signer.getWarnings()));
        result.getV3SchemeSigners().forEach(signer -> warnings.addAll(signer.getWarnings()));
        result.getV31SchemeSigners().forEach(signer -> warnings.addAll(signer.getWarnings()));
        result.getV4SchemeSigners().forEach(signer -> warnings.addAll(signer.getWarnings()));
        if (!warnings.isEmpty()) {
            throw new IllegalArgumentException("APK signature verification warnings: " + warnings);
        }
        if (!result.isVerifiedUsingV1Scheme() || !result.isVerifiedUsingV2Scheme()
                || !result.isVerifiedUsingV3Scheme() || !result.isVerifiedUsingV4Scheme()) {
            throw new IllegalArgumentException("Expected verified v1, v2, v3 and v4 APK signatures");
        }
        if (result.getSignerCertificates().size() != 1) {
            throw new IllegalArgumentException("Expected exactly one APK signer");
        }
        byte[] certificate = result.getSignerCertificates().get(0).getEncoded();
        String digest = HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(certificate));
        if (args.length == 2 && !RELEASE_CERTIFICATE.equals(digest)) {
            throw new IllegalArgumentException("APK signer differs from the LexiGo release certificate: " + digest);
        }
        System.out.println("APK signatures v1/v2/v3/v4 verified. Signer SHA-256: " + digest);
    }
}
