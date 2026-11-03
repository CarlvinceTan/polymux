import Foundation
import Testing
@testable import PolymuxKit

struct InviteTests {
    @Test func linkRoundTripsWithAndWithoutTicket() throws {
        for ticket in [nil, "endpointabc"] as [String?] {
            let invite = Invite(url: "http://192.168.1.20:7337", token: "a+b/c=", ticket: ticket)
            #expect(Invite(link: invite.link) == invite)
        }
    }

    @Test func rejectsOtherLinks() throws {
        for text in ["https://polymux.app/connect?url=x&token=y", "polymux://settings", "polymux://connect?url=x"] {
            #expect(Invite(link: try #require(URL(string: text))) == nil)
        }
    }

    @Test(arguments: ["not-a-url", "ftp://server", "http://", "http://user:pass@server", "http://server:0", "http://server:65536", "http://server:", "http://server?secret=x", "http://server#fragment"])
    func rejectsInvalidEmbeddedAddress(address: String) {
        #expect(Invite(link: Invite(url: address, token: "test").link) == nil)
    }

    @Test(arguments: ["", " ", "token\n", "a\0b"])
    func rejectsEmptyOrUnsafeCredential(token: String) {
        #expect(Invite(link: Invite(url: "https://server.example", token: token).link) == nil)
    }

    @Test func rejectsAmbiguousParameters() throws {
        for value in ["polymux://connect?url=http://one&url=http://two&token=test", "polymux://connect?url=http://one&token=a&token=b", "polymux://connect?url=http://one&token=a&ticket=", "polymux://connect/other?url=http://one&token=a"] {
            #expect(Invite(link: try #require(URL(string: value))) == nil)
        }
    }
}
