import XCTest
@testable import MSeismo

final class CrashReportTests: XCTestCase {
    func testRoundTripsThroughJSON() throws {
        let crash = CrashReport(
            name: "NSInvalidArgumentException",
            reason: "-[__NSArrayI objectAtIndex:]: index out of bounds",
            callStack: ["0 CoreFoundation 0x1", "1 libobjc.A.dylib 0x2"],
            isFatal: true
        )

        let data = try JSONEncoder().encode(crash)
        let decoded = try JSONDecoder().decode(CrashReport.self, from: data)

        XCTAssertEqual(decoded, crash)
    }

    func testAsReportBuildsACrashCategorizedReport() {
        let crash = CrashReport(
            name: "NSInvalidArgumentException",
            reason: "index out of bounds",
            callStack: ["frame 1", "frame 2"],
            isFatal: true
        )

        let report = crash.asReport()

        XCTAssertEqual(report.category, .crash)
        XCTAssertEqual(report.severity, .critical)
        XCTAssertEqual(report.title, "NSInvalidArgumentException")
        XCTAssertTrue(report.description.contains("index out of bounds"))
        XCTAssertTrue(report.description.contains("frame 1"))
    }

    func testAsReportCarriesTheCrashDetailsTriggerAndTime() {
        let when = Date(timeIntervalSince1970: 1_000)
        let fatal = CrashReport(name: "SIGSEGV", reason: nil, callStack: ["frame"], isFatal: true, timestamp: when)
        let nonFatal = CrashReport(name: "SomeError", reason: nil, callStack: ["frame"], isFatal: false, timestamp: when)

        XCTAssertEqual(fatal.asReport().trigger, .crash)
        XCTAssertEqual(nonFatal.asReport().trigger, .nonFatal)
        XCTAssertEqual(fatal.asReport().crash, fatal)
        XCTAssertEqual(fatal.asReport().occurredAt, when)
    }

    func testAsReportHandlesMissingReason() {
        let crash = CrashReport(name: "SIGSEGV", reason: nil, callStack: [], isFatal: true)

        let report = crash.asReport()

        XCTAssertEqual(report.title, "SIGSEGV")
    }
}
