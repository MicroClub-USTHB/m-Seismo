package org.microclub.mseismo;

import static org.junit.Assert.assertEquals;

import android.app.Activity;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import org.junit.After;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

// Compiling this file is the test: it uses the public API the way a Java app would.
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 34)
public class JavaApiTest {
    @After
    public void resetRuntimeState() {
        MSeismo.setFeatureFlags(Collections.emptyMap());
        MSeismo.setExperiments(Collections.emptyMap());
        MSeismo.setCrashlyticsInstallationId(null);
        MSeismo.setOnAutoReport(null);
    }

    @Test
    public void publicApiIsUsableFromJava() {
        Configuration config = new Configuration("https://seismo.example.com", "msk_test");
        assertEquals("production", config.getEnvironment());
        Configuration staging = new Configuration("https://seismo.example.com", "msk_test", "staging");
        assertEquals("staging", staging.getEnvironment());
        assertEquals(4, Configuration.DEFAULT_REDACTION_RULES.size());
        config.validate(false);

        MSeismo.log("from Java");
        MSeismo.breadcrumb(BreadcrumbKind.TAP, "Java button");
        MSeismo.recordRequest("https://api.example.com", "GET", 200, 10L);
        MSeismo.report();
        MSeismo.report("title");
        MSeismo.report("title", "description", Category.BUG, Severity.HIGH);
        MSeismo.recordError(new IllegalStateException("from Java"));
        MSeismo.recordError("message", "frame", true);
        MSeismo.checkForPendingCrash();
        MSeismo.screen("Checkout");
        MSeismo.setAccentColor(0xFF5B3CC4);
        MSeismo.setAccentColor(null);
        MSeismo.showReportScreen(Robolectric.buildActivity(Activity.class).setup().get());
        MSeismo.showReportScreen(
                Robolectric.buildActivity(Activity.class).setup().get(),
                Trigger.SHAKE
        );
        MSeismo.getSessionId();
        MSeismo.getSDK_VERSION();
        MSeismo.getConfiguration();
        MSeismo.getRelease();
        MSeismo.getInstallationId();

        MSeismo.setCapturesNetworkBodies(true);
        MSeismo.setRedactionAllowList(List.of("support@example.com"));
        MSeismo.setRedactionDenyList(List.of("ProjectX"));

        Map<String, FlagValue> flags = new HashMap<>();
        flags.put("beta", new FlagValue.BooleanValue(true));
        flags.put("rollout", new FlagValue.NumberValue(0.5));
        flags.put("variant", new FlagValue.StringValue("blue"));
        MSeismo.setFeatureFlags(flags);
        MSeismo.setExperiments(Collections.singletonMap("checkout", "v2"));
        MSeismo.setCrashlyticsInstallationId("crashlytics-id");
        MSeismo.setOnAutoReport(report -> kotlin.Unit.INSTANCE);

        Crash crash = new Crash("java.lang.RuntimeException", "boom", List.of("frame"));
        MSeismo.submit(
                Trigger.CRASH,
                "title",
                "description",
                Category.CRASH,
                Severity.CRITICAL,
                new byte[] {1, 2, 3},
                crash,
                1L,
                true,
                "11111111-1111-4111-8111-111111111111",
                "session-id"
        );

        MSeismo.pendingReportCount();
        MSeismo.retryNow();
        MSeismo.newTraceId();
        MSeismo.getFeatureFlags();
        MSeismo.getExperiments();
        MSeismo.getCrashlyticsInstallationId();
        MSeismo.getOnAutoReport();
    }
}
