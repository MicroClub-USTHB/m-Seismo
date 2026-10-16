import SwiftUI
import MSeismo

@main
struct ExampleApp: App {
    init() {
        MSeismo.shared.configure(projectKey: "example-project-key", environment: "staging")
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}
