#if os(iOS)
import Foundation
import Network
import UIKit

extension DeviceContext {
    /// Captures the current device/app context (FR-15). Network type is a
    /// best-effort snapshot: it waits up to 200ms for `NWPathMonitor`'s
    /// first update and falls back to "unknown" rather than block longer,
    /// since this runs synchronously on report submission.
    public static func current() -> DeviceContext {
        let device = UIDevice.current
        device.isBatteryMonitoringEnabled = true

        let bundle = Bundle.main
        let appVersion = bundle.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown"
        let appBuild = bundle.infoDictionary?["CFBundleVersion"] as? String ?? "unknown"

        return DeviceContext(
            osVersion: device.systemVersion,
            deviceModel: modelIdentifier(),
            locale: Locale.current.identifier,
            batteryLevel: device.batteryLevel >= 0 ? device.batteryLevel : nil,
            availableMemoryBytes: ProcessInfo.processInfo.physicalMemory,
            networkType: currentNetworkType(),
            orientation: orientationDescription(device.orientation),
            appVersion: appVersion,
            appBuild: appBuild
        )
    }

    private static func modelIdentifier() -> String {
        var systemInfo = utsname()
        uname(&systemInfo)
        return withUnsafePointer(to: &systemInfo.machine) { pointer in
            pointer.withMemoryRebound(to: CChar.self, capacity: 1) { String(cString: $0) }
        }
    }

    private static func orientationDescription(_ orientation: UIDeviceOrientation) -> String {
        switch orientation {
        case .portrait: return "portrait"
        case .portraitUpsideDown: return "portraitUpsideDown"
        case .landscapeLeft: return "landscapeLeft"
        case .landscapeRight: return "landscapeRight"
        case .faceUp: return "faceUp"
        case .faceDown: return "faceDown"
        default: return "unknown"
        }
    }

    private static func currentNetworkType() -> String {
        let monitor = NWPathMonitor()
        let semaphore = DispatchSemaphore(value: 0)
        var result = "unknown"

        monitor.pathUpdateHandler = { path in
            if path.usesInterfaceType(.wifi) {
                result = "wifi"
            } else if path.usesInterfaceType(.cellular) {
                result = "cellular"
            } else if path.status == .satisfied {
                result = "other"
            } else {
                result = "offline"
            }
            semaphore.signal()
        }

        let queue = DispatchQueue(label: "mseismo.network-type")
        monitor.start(queue: queue)
        _ = semaphore.wait(timeout: .now() + 0.2)
        monitor.cancel()

        return result
    }
}
#endif
