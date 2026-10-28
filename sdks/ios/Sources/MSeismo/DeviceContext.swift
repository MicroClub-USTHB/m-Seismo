import Foundation

/// Automatic device/app context attached to every report (PRD FR-15).
/// The type itself is platform-agnostic (so `Report` can hold it
/// unconditionally); `DeviceContext.current()`, which does the actual
/// capturing, is iOS-only (see `DeviceContext+iOS.swift`).
public struct DeviceContext: Codable, Equatable {
    public var osVersion: String
    public var deviceModel: String
    public var locale: String
    public var batteryLevel: Float?
    public var availableMemoryBytes: UInt64
    public var networkType: String
    public var orientation: String
    public var appVersion: String
    public var appBuild: String

    public init(
        osVersion: String,
        deviceModel: String,
        locale: String,
        batteryLevel: Float?,
        availableMemoryBytes: UInt64,
        networkType: String,
        orientation: String,
        appVersion: String,
        appBuild: String
    ) {
        self.osVersion = osVersion
        self.deviceModel = deviceModel
        self.locale = locale
        self.batteryLevel = batteryLevel
        self.availableMemoryBytes = availableMemoryBytes
        self.networkType = networkType
        self.orientation = orientation
        self.appVersion = appVersion
        self.appBuild = appBuild
    }
}
