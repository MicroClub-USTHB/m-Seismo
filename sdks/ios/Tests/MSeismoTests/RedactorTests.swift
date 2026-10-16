import XCTest
@testable import MSeismo

final class RedactorTests: XCTestCase {
    func testRedactsEmailAddresses() {
        let redactor = Redactor()

        let result = redactor.redact("Contact me at jane.doe@example.com for details")

        XCTAssertEqual(result, "Contact me at [REDACTED] for details")
    }

    func testRedactsPhoneNumbers() {
        let redactor = Redactor()

        let result = redactor.redact("Call 415-555-0123 tomorrow")

        XCTAssertEqual(result, "Call [REDACTED] tomorrow")
    }

    func testRedactsCreditCardNumbers() {
        let redactor = Redactor()

        let result = redactor.redact("Card on file: 4111 1111 1111 1111")

        XCTAssertEqual(result, "Card on file: [REDACTED]")
    }

    func testRedactsBearerTokens() {
        let redactor = Redactor()

        let result = redactor.redact("Authorization: Bearer abc123XYZ789secretTokenValue")

        XCTAssertEqual(result, "Authorization: [REDACTED]")
    }

    func testLeavesOrdinaryTextAlone() {
        let redactor = Redactor()

        let result = redactor.redact("The login button is misaligned on iPad")

        XCTAssertEqual(result, "The login button is misaligned on iPad")
    }

    func testDenyListRedactsAdditionalLiteralStrings() {
        let redactor = Redactor()
        redactor.denyList = ["internal-project-codename"]

        let result = redactor.redact("This affects internal-project-codename users")

        XCTAssertEqual(result, "This affects [REDACTED] users")
    }

    func testAllowListProtectsMatchingTextFromRedaction() {
        let redactor = Redactor()
        redactor.allowList = ["support@example.com"]

        let result = redactor.redact("Email support@example.com for help")

        XCTAssertEqual(result, "Email support@example.com for help")
    }

    func testAllowListOnlyProtectsTheListedString() {
        let redactor = Redactor()
        redactor.allowList = ["support@example.com"]

        let result = redactor.redact("support@example.com or jane.doe@example.com")

        XCTAssertEqual(result, "support@example.com or [REDACTED]")
    }
}
