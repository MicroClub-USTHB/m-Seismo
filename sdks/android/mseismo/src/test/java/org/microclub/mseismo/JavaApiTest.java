package org.microclub.mseismo;

import static org.junit.Assert.assertEquals;

import android.app.Activity;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.Robolectric;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

// Compiling this file is the test: it uses the public API the way a Java app would.
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 34)
public class JavaApiTest {
    @Test
    public void publicApiIsUsableFromJava() {
        Configuration config = new Configuration("https://seismo.example.com", "msk_test");
        assertEquals("production", config.getEnvironment());
        Configuration staging = new Configuration("https://seismo.example.com", "msk_test", "staging");
        assertEquals("staging", staging.getEnvironment());
        assertEquals(4, Configuration.DEFAULT_REDACTION_RULES.size());

        MSeismo.log("from Java");
        MSeismo.breadcrumb(BreadcrumbKind.TAP, "Java button");
        MSeismo.recordRequest("https://api.example.com", "GET", 200, 10L);
        MSeismo.report();
        MSeismo.report("title");
        MSeismo.report("title", "description", Category.BUG, Severity.HIGH);
        MSeismo.showReportScreen(Robolectric.buildActivity(Activity.class).setup().get());
        MSeismo.getSessionId();
        MSeismo.getSDK_VERSION();
        MSeismo.getConfiguration();
        MSeismo.getRelease();
        MSeismo.getInstallationId();
    }
}
