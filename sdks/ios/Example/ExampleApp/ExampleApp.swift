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
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}
