import SwiftUI
import MSeismo

struct ContentView: View {
    @ObservedObject private var mseismo = MSeismo.shared

    var body: some View {
        VStack(spacing: 12) {
            Text("m-Seismo Example")
                .font(.headline)
            Text("Project key: \(mseismo.projectKey ?? "none")")
            Text("Environment: \(mseismo.environment ?? "none")")
            Text("Shake the device (or Hardware > Shake Gesture in the Simulator) to open the report form.")
                .font(.caption)
                .multilineTextAlignment(.center)
        }
        .padding()
        .onShake {
            mseismo.shakeDetector.shakeDetected()
        }
        .sheet(isPresented: $mseismo.isReportSheetPresented) {
            ReportFormView(
                onSubmit: { report in
                    print("Report submitted: \(report)")
                    mseismo.isReportSheetPresented = false
                },
                onCancel: {
                    mseismo.isReportSheetPresented = false
                }
            )
        }
    }
}
