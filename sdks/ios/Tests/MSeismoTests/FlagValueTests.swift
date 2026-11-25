import XCTest
@testable import MSeismo

final class FlagValueTests: XCTestCase {
    func testRoundTripsEachKind() throws {
        let flags: [String: FlagValue] = ["on": true, "count": 3, "ratio": 0.5, "name": "dark"]

        let data = try JSONEncoder().encode(flags)
        let decoded = try JSONDecoder().decode([String: FlagValue].self, from: data)

        XCTAssertEqual(decoded, flags)
    }

    func testBooleanIsNotReadAsANumber() throws {
        let decoded = try JSONDecoder().decode([String: FlagValue].self, from: Data(#"{"a":true,"b":1}"#.utf8))

        XCTAssertEqual(decoded["a"], .bool(true))
        XCTAssertEqual(decoded["b"], .number(1))
    }

    func testReportWithFlagsSurvivesTheOfflineQueueStore() throws {
        let report = Report(title: "Saved", featureFlags: ["x": true], experiments: ["e": "v"], crashlyticsInstallationID: "fid")

        let data = try JSONEncoder().encode(report)
        let decoded = try JSONDecoder().decode(Report.self, from: data)

        XCTAssertEqual(decoded, report)
    }
}
