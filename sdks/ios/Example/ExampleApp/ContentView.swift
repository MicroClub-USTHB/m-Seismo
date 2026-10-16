import SwiftUI
import MSeismo

struct ContentView: View {
    var body: some View {
        VStack(spacing: 12) {
            Text("m-Seismo Example")
                .font(.headline)
            Text("Project key: \(MSeismo.shared.projectKey ?? "none")")
            Text("Environment: \(MSeismo.shared.environment ?? "none")")
        }
        .padding()
    }
}
