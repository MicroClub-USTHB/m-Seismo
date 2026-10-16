import SwiftUI
import MSeismo

@main
struct ExampleApp: App {
    init() {
        MSeismo.shared.configure(
            Configuration(
                endpoint: URL(string: "https://seismo.example.com")!,
                projectKey: "example-project-key",
                environment: "staging"
            )
        )

        MSeismo.shared.onAutoReport = { report in
            print("Auto-reported (no sheet shown): \(report.title) — \(report.description)")
        }
        MSeismo.shared.checkForPendingCrash()
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}
