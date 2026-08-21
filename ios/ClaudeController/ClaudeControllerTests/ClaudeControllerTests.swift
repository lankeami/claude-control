//
//  ClaudeControllerTests.swift
//  ClaudeControllerTests
//
//  Created by Jay Chinthrajah on 3/17/26.
//

import Foundation
import Testing
@testable import ClaudeController

struct ClaudeControllerTests {

    @Test func example() async throws {
        // Write your test here and use APIs like `#expect(...)` to check expected conditions.
    }

    @Test func sessionDecodesWithoutAgent() throws {
        let json = """
        {
            "id": "abc-123",
            "computer_name": "mac",
            "project_path": "/tmp/test",
            "name": "",
            "status": "active",
            "created_at": "2026-01-01T00:00:00Z",
            "last_seen_at": "2026-01-01T00:00:00Z",
            "archived": false
        }
        """.data(using: .utf8)!

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let session = try decoder.decode(Session.self, from: json)
        #expect(session.agent == "claude")
    }

    @Test func sessionDecodesWithAgent() throws {
        let json = """
        {
            "id": "abc-456",
            "computer_name": "mac",
            "project_path": "/tmp/test",
            "name": "",
            "status": "active",
            "created_at": "2026-01-01T00:00:00Z",
            "last_seen_at": "2026-01-01T00:00:00Z",
            "archived": false,
            "agent": "codex"
        }
        """.data(using: .utf8)!

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let session = try decoder.decode(Session.self, from: json)
        #expect(session.agent == "codex")
    }

}
