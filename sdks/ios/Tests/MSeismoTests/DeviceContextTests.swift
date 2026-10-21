import XCTest
@testable import MSeismo

final class DeviceContextTests: XCTestCase {
    func testStoresAllFields() {
        let context = DeviceContext(
            osVersion: "17.4",
            deviceModel: "iPhone15,2",
            locale: "en_US",
            batteryLevel: 0.82,
            availableMemoryBytes: 4_000_000_000,
            networkType: "wifi",
            orientation: "portrait",
            appVersion: "1.2.3",
            appBuild: "45"
        )

        XCTAssertEqual(context.osVersion, "17.4")
        XCTAssertEqual(context.deviceModel, "iPhone15,2")
        XCTAssertEqual(context.locale, "en_US")
        XCTAssertEqual(context.batteryLevel, 0.82)
        XCTAssertEqual(context.availableMemoryBytes, 4_000_000_000)
        XCTAssertEqual(context.networkType, "wifi")
        XCTAssertEqual(context.orientation, "portrait")
        XCTAssertEqual(context.appVersion, "1.2.3")
        XCTAssertEqual(context.appBuild, "45")
    }

    func testBatteryLevelCanBeNilWhenMonitoringIsUnavailable() {
        let context = DeviceContext(
            osVersion: "17.4",
            deviceModel: "iPhone15,2",
            locale: "en_US",
            batteryLevel: nil,
            availableMemoryBytes: 4_000_000_000,
            networkType: "wifi",
            orientation: "portrait",
            appVersion: "1.2.3",
            appBuild: "45"
        )

        XCTAssertNil(context.batteryLevel)
    }

    func testRoundTripsThroughJSON() throws {
        let context = DeviceContext(
            osVersion: "17.4",
            deviceModel: "iPhone15,2",
            locale: "en_US",
            batteryLevel: 0.5,
            availableMemoryBytes: 4_000_000_000,
            networkType: "cellular",
            orientation: "landscapeLeft",
            appVersion: "1.2.3",
            appBuild: "45"
        )

        let data = try JSONEncoder().encode(context)
        let decoded = try JSONDecoder().decode(DeviceContext.self, from: data)

        XCTAssertEqual(decoded, context)
    }
}
