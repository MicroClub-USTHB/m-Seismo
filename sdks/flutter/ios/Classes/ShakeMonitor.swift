import CoreMotion
import Foundation

/// Feeds physical-device accelerometer shakes into the native
/// `ShakeDetector`, which owns the enabled flag and the cooldown.
///
/// The app's Info.plist must contain `NSMotionUsageDescription`. CoreMotion
/// is not available in the iOS Simulator, so shake triggers can only be
/// verified on a physical device.
final class ShakeMonitor {
    private let motionManager = CMMotionManager()
    private let threshold = 2.5
    private let hysteresis = 1.5
    private var isOverThreshold = false
    private var isRunning = false

    func start() {
        guard !isRunning, motionManager.isAccelerometerAvailable else { return }
        isRunning = true
        motionManager.accelerometerUpdateInterval = 0.1
        motionManager.startAccelerometerUpdates(to: .main) { [weak self] data, _ in
            guard let self, let acceleration = data?.acceleration else { return }
            let magnitude = sqrt(
                acceleration.x * acceleration.x +
                    acceleration.y * acceleration.y +
                    acceleration.z * acceleration.z
            )

            if magnitude >= self.threshold {
                if !self.isOverThreshold {
                    self.isOverThreshold = true
                    MSeismo.shared.shakeDetector.shakeDetected()
                }
            } else if magnitude <= self.hysteresis {
                self.isOverThreshold = false
            }
        }
    }

    func stop() {
        guard isRunning else { return }
        isRunning = false
        isOverThreshold = false
        motionManager.stopAccelerometerUpdates()
    }

    deinit {
        stop()
    }
}
