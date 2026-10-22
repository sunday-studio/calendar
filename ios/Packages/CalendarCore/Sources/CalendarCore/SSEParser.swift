import Foundation

/// Incremental parser for `text/event-stream` (WHATWG). Feed raw bytes; get complete events out.
/// Done byte-wise because `AsyncBytes.lines` drops the blank lines that delimit frames.
public struct SSEParser: Sendable {
    private var line: [UInt8] = []
    private var id: String?
    private var name: String?
    private var data: [String] = []
    private var sawCR = false

    public init() {}

    public mutating func feed(_ bytes: some Sequence<UInt8>) -> [StreamEvent] {
        var out: [StreamEvent] = []
        for b in bytes { if let e = feed(byte: b) { out.append(e) } }
        return out
    }

    public mutating func feed(byte b: UInt8) -> StreamEvent? {
        if b == 0x0A {
            if sawCR { sawCR = false; return nil } // second half of CRLF
            return endOfLine()
        }
        if b == 0x0D { sawCR = true; return endOfLine() }
        sawCR = false
        line.append(b)
        return nil
    }

    private mutating func endOfLine() -> StreamEvent? {
        defer { line.removeAll(keepingCapacity: true) }
        let text = String(decoding: line, as: UTF8.self)
        if text.isEmpty { return dispatch() }
        if text.hasPrefix(":") { return nil }
        let field: Substring, value: Substring
        if let i = text.firstIndex(of: ":") {
            field = text[..<i]
            var v = text[text.index(after: i)...]
            if v.first == " " { v = v.dropFirst() }
            value = v
        } else { field = Substring(text); value = "" }
        switch field {
        case "id": id = String(value)
        case "event": name = String(value)
        case "data": data.append(String(value))
        default: break // retry and unknown fields ignored
        }
        return nil
    }

    private mutating func dispatch() -> StreamEvent? {
        defer { id = nil; name = nil; data = [] }
        guard let name else { return nil }
        let payload = Data(data.joined(separator: "\n").utf8)
        let d = JSONDecoder()
        switch name {
        case "ping": return StreamEvent(id: nil, kind: .ping)
        case "rev":
            struct P: Decodable { var rev: Int64 }
            guard let p = try? d.decode(P.self, from: payload) else { return nil }
            return StreamEvent(id: id, kind: .rev(p.rev))
        case "account":
            struct P: Decodable { var id: String; var status: String }
            guard let p = try? d.decode(P.self, from: payload) else { return nil }
            return StreamEvent(id: id, kind: .account(id: p.id, status: p.status))
        case "sync":
            struct P: Decodable { var accountId: String; var state: String }
            guard let p = try? d.decode(P.self, from: payload) else { return nil }
            return StreamEvent(id: id, kind: .sync(accountId: p.accountId, state: p.state))
        default: return nil // unknown event names are tolerated
        }
    }
}
