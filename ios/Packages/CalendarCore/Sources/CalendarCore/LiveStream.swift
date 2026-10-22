import Foundation

/// Reconnect delays: initial * 2^attempt, capped, with +-jitter (fraction). Deterministic when `jitter == 0`.
public struct Backoff: Sendable {
    public var initial: TimeInterval = 1
    public var cap: TimeInterval = 30
    public var jitter: Double = 0.2
    public init(initial: TimeInterval = 1, cap: TimeInterval = 30, jitter: Double = 0.2) {
        self.initial = initial; self.cap = cap; self.jitter = jitter
    }
    public func delay(attempt: Int) -> TimeInterval {
        let base = min(cap, initial * pow(2, Double(min(attempt, 20))))
        guard jitter > 0 else { return base }
        return base * (1 + Double.random(in: -jitter...jitter))
    }
}

/// Keeps one SSE connection alive: reconnects with exponential backoff, resuming with `Last-Event-ID`.
/// The connection is owned by an actor-managed task; `events` is the single consumer stream.
public actor LiveStream {
    public typealias Connect = @Sendable (_ lastEventID: String?) -> AsyncThrowingStream<StreamEvent, Error>
    public typealias Sleep = @Sendable (TimeInterval) async throws -> Void

    public nonisolated let events: AsyncStream<StreamEvent>
    private let continuation: AsyncStream<StreamEvent>.Continuation
    private let connect: Connect
    private let sleep: Sleep
    private let backoff: Backoff
    private var lastEventID: String?
    private var task: Task<Void, Never>?
    public private(set) var connectionCount = 0

    public init(
        lastEventID: String? = nil, backoff: Backoff = Backoff(), connect: @escaping Connect,
        sleep: @escaping Sleep = { try await Task.sleep(for: .seconds($0)) }
    ) {
        (events, continuation) = AsyncStream.makeStream(of: StreamEvent.self, bufferingPolicy: .unbounded)
        self.lastEventID = lastEventID
        self.connect = connect
        self.sleep = sleep
        self.backoff = backoff
    }

    public func start() {
        guard task == nil else { return }
        task = Task { await self.run() }
    }

    /// Terminal: cancels the connection and finishes `events`.
    public func stop() {
        task?.cancel()
        task = nil
        continuation.finish()
    }

    private func run() async {
        var attempt = 0
        while !Task.isCancelled {
            connectionCount += 1
            do {
                for try await e in connect(lastEventID) {
                    attempt = 0 // healthy connection: reset backoff
                    if let id = e.id { lastEventID = id }
                    continuation.yield(e)
                }
            } catch is CancellationError {
                return
            } catch {}
            if Task.isCancelled { return }
            let d = backoff.delay(attempt: attempt)
            attempt += 1
            do { try await sleep(d) } catch { return }
        }
    }
}
