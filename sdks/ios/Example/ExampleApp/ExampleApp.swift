import SwiftUI
import MSeismo

@main
struct ExampleApp: App {
    init() {
        MSeismo.shared.configure(
            Configuration(
                // 127.0.0.1, not "localhost": the device-literal loopback
                // address, reachable from the Simulator without any
                // networking setup. See fake_server.py.
                endpoint: URL(string: "http://127.0.0.1:8787/reports")!,
                projectKey: "example-project-key",
                environment: "staging",
                screenshotEnabled: true
            )
        )

        MSeismo.shared.onAutoReport = { report in
            print("Auto-reported (no sheet shown): \(report.title) — \(report.description)")
        }
        MSeismo.shared.checkForPendingCrash()

        // Exercises the network layer + offline queue on every launch,
        // without needing a UI interaction -- see fake_server.py.
        let submitted = MSeismo.shared.submit(Report(
            title: "Auto test report on launch",
            description: "Verifying SDK-10's transport + offline queue"
        ))
        print("Submitted for delivery: \(submitted.title)")
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}
