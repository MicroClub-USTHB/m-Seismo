import XCTest
@testable import MSeismo

final class CrashStoreTests: XCTestCase {
    private func makeStore() -> CrashStore {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        return CrashStore(directory: directory)
    }

    func testLoadAndClearReturnsNilWhenNothingSaved() {
        let store = makeStore()

        XCTAssertNil(store.loadAndClear())
    }

    func testSaveThenLoadAndClearReturnsTheCrash() {
        let store = makeStore()
        let crash = CrashReport(name: "NSRangeException", reason: "boom", callStack: ["frame"], isFatal: true)

        store.save(crash)

        XCTAssertEqual(store.loadAndClear(), crash)
    }

    func testLoadAndClearConsumesTheSavedCrash() {
        let store = makeStore()
        let crash = CrashReport(name: "NSRangeException", reason: "boom", callStack: [], isFatal: true)
        store.save(crash)

        _ = store.loadAndClear()

        XCTAssertNil(store.loadAndClear())
    }
}
