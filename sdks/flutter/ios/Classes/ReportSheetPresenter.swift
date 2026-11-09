import Combine
import SwiftUI
import UIKit

/// Observes the native SDK's published sheet state and presents the SwiftUI
/// report form from the active Flutter view controller.
final class ReportSheetPresenter: NSObject, UIAdaptivePresentationControllerDelegate {
    private var cancellable: AnyCancellable?
    private weak var presentedController: UIViewController?
    private var isPresenting = false

    func start() {
        cancellable = MSeismo.shared.$isReportSheetPresented
            .receive(on: RunLoop.main)
            .sink { [weak self] isPresented in
                guard let self else { return }
                if isPresented {
                    self.presentIfNeeded()
                } else {
                    self.dismissIfNeeded()
                }
            }
    }

    func stop() {
        cancellable?.cancel()
        cancellable = nil
        dismissIfNeeded()
    }

    private func presentIfNeeded() {
        guard !isPresenting, presentedController == nil else { return }
        guard let top = Self.topViewController() else { return }
        guard top.presentedViewController == nil else { return }

        let sheet = UIHostingController(
            rootView: ReportFormView(
                initial: Report(screenshot: MSeismo.shared.capturedScreenshot),
                onSubmit: { [weak self] _ in self?.finishPresentation() },
                onCancel: { [weak self] in self?.finishPresentation() }
            )
        )
        sheet.modalPresentationStyle = .pageSheet
        sheet.presentationController?.delegate = self

        isPresenting = true
        presentedController = sheet
        top.present(sheet, animated: true) { [weak self, weak sheet] in
            self?.isPresenting = false
            sheet?.presentationController?.delegate = self
        }
    }

    private func finishPresentation() {
        MSeismo.shared.isReportSheetPresented = false
        dismissIfNeeded()
    }

    private func dismissIfNeeded() {
        guard let controller = presentedController else { return }
        presentedController = nil
        isPresenting = false
        controller.presentingViewController?.dismiss(animated: true)
    }

    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) {
        presentedController = nil
        isPresenting = false
        MSeismo.shared.isReportSheetPresented = false
    }

    private static func topViewController() -> UIViewController? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let scene = scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
        let window = scene?.windows.first { $0.isKeyWindow } ?? scene?.windows.first

        var controller = window?.rootViewController
        while let presented = controller?.presentedViewController {
            controller = presented
        }

        if let navigation = controller as? UINavigationController {
            return navigation.visibleViewController ?? navigation
        }
        if let tab = controller as? UITabBarController {
            return tab.selectedViewController ?? tab
        }
        return controller
    }
}
