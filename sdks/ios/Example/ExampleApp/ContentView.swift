import SwiftUI
import MSeismo

struct ContentView: View {
    @ObservedObject private var mseismo = MSeismo.shared

    var body: some View {
        VStack(spacing: 12) {
            Text("m-Seismo Example")
                .font(.headline)
            Text("Project key: \(mseismo.configuration?.projectKey ?? "none")")
            Text("Environment: \(mseismo.configuration?.environment ?? "none")")
            Text("Shake the device (or Device > Shake in the Simulator), take a screenshot, long-press this text, or use the button below to open the report form.")
                .font(.caption)
                .multilineTextAlignment(.center)
                .onLongPressGesture {
                    mseismo.report()
                }
            Button("Report a bug") {
                mseismo.report()
            }
        }
        .padding()
        .onShake {
            mseismo.shakeDetector.shakeDetected()
        }
        .sheet(isPresented: $mseismo.isReportSheetPresented) {
            ReportFormView(
                onSubmit: { report in
                    print("Report submitted: \(report)")
                    if let context = report.deviceContext {
                        print("Device context: \(context)")
                    }
                    mseismo.isReportSheetPresented = false
                },
                onCancel: {
                    mseismo.isReportSheetPresented = false
                }
            )
        }
    }
}
