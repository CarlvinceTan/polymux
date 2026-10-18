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
}
