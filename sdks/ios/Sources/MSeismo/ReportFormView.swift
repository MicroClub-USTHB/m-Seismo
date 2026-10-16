#if os(iOS)
import SwiftUI
import UIKit

/// The in-app report sheet (PRD FR-10). Every field is optional (FR-12);
/// the reporter can dismiss it at any time without sending (FR-13).
///
/// Written against iOS 13 APIs only (no `TextEditor`/`.toolbar`/`Label`,
/// which need iOS 14+) to match this package's deployment target. Colors
/// are left to the system default (`accentColor`) rather than hardcoded,
/// so the sheet picks up whatever theme the host app has set.
public struct ReportFormView: View {
    @State private var report: Report
    @State private var screenshot: UIImage?
    @State private var isScreenshotPickerPresented = false

    let onSubmit: (Report) -> Void
    let onCancel: () -> Void

    public init(
        initial: Report = Report(),
        onSubmit: @escaping (Report) -> Void,
        onCancel: @escaping () -> Void
    ) {
        _report = State(initialValue: initial)
        _screenshot = State(initialValue: initial.screenshot.flatMap(UIImage.init(data:)))
        self.onSubmit = onSubmit
        self.onCancel = onCancel
    }

    public var body: some View {
        NavigationView {
            Form {
                Section(header: Text("Details")) {
                    TextField("Title", text: $report.title)
                    TextField("Description", text: $report.description)
                }

                Section(header: Text("Classification")) {
                    Picker("Category", selection: $report.category) {
                        ForEach(Report.Category.allCases) { category in
                            Text(category.label).tag(category)
                        }
                    }

                    VStack(alignment: .leading, spacing: 6) {
                        Text("Severity")
                            .font(.caption)
                            .foregroundColor(.secondary)
                        Picker("Severity", selection: $report.severity) {
                            ForEach(Report.Severity.allCases) { severity in
                                Text(severity.label).tag(severity)
                            }
                        }
                        .pickerStyle(SegmentedPickerStyle())
                    }
                    .padding(.vertical, 4)
                }

                Section(header: Text("Screenshot")) {
                    if let screenshot = screenshot {
                        HStack {
                            Image(uiImage: screenshot)
                                .resizable()
                                .scaledToFill()
                                .frame(width: 56, height: 56)
                                .clipShape(RoundedRectangle(cornerRadius: 8))
                                .clipped()

                            Spacer()

                            Button("Remove") {
                                self.screenshot = nil
                            }
                        }
                    } else {
                        Button {
                            isScreenshotPickerPresented = true
                        } label: {
                            HStack {
                                Image(systemName: "photo.on.rectangle")
                                Text("Add Screenshot")
                            }
                        }
                    }
                }
            }
            .navigationBarTitle("Report a bug", displayMode: .inline)
            .navigationBarItems(
                leading: Button("Cancel", action: onCancel)
                    .foregroundColor(.accentColor),
                trailing: Button("Send") {
                    report.screenshot = screenshot?.jpegData(compressionQuality: 0.8)
                    onSubmit(report)
                }
                .foregroundColor(.accentColor)
            )
            .sheet(isPresented: $isScreenshotPickerPresented) {
                ScreenshotPicker { image in
                    screenshot = image
                    isScreenshotPickerPresented = false
                }
            }
        }
    }
}
#endif
