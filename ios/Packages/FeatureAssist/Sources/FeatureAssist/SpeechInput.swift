import AVFoundation
import Foundation
import Observation
import Speech

/// What the mic UI needs from speech input. The real one is on-device only; tests script this.
@MainActor
public protocol SpeechTranscribing: AnyObject {
    /// Asks for microphone + speech permission (once); returns the state afterwards.
    func requestAccess() async -> SpeechAccess
    /// Starts listening. `update(transcript, level)` is called as words and loudness (0...1) arrive.
    func start(update: @escaping @MainActor (String, Double) -> Void) throws
    func stop()
}

public enum SpeechAccess: Equatable, Sendable {
    case ready
    /// Microphone or speech recognition refused: "Allow the microphone in Settings, or type it instead."
    case denied
    /// No on-device model for this language: nothing is sent to Apple's servers, so we do not listen.
    case unavailable
}

/// Voice sheet state (canvas q-voice).
@MainActor
@Observable
public final class VoiceViewModel {
    public enum State: Equatable, Sendable { case idle, listening, denied, unavailable, failed }

    public private(set) var state: State = .idle
    public private(set) var transcript = ""
    /// Eight bars, newest last.
    public private(set) var levels: [Double] = Array(repeating: 0.08, count: 8)

    @ObservationIgnored private let engine: any SpeechTranscribing

    public init(engine: any SpeechTranscribing) { self.engine = engine }

    public var hasWords: Bool { !transcript.trimmingCharacters(in: .whitespaces).isEmpty }

    public func begin() async {
        guard state != .listening else { return }
        transcript = ""
        levels = Array(repeating: 0.08, count: 8)
        switch await engine.requestAccess() {
        case .denied: state = .denied; return
        case .unavailable: state = .unavailable; return
        case .ready: break
        }
        do {
            try engine.start { [weak self] text, level in
                guard let self, self.state == .listening else { return }
                self.transcript = text
                self.levels = Array((self.levels + [max(0.08, min(level, 1))]).suffix(8))
            }
            state = .listening
        } catch {
            state = .failed
        }
    }

    /// "Done": the words so far.
    public func finish() -> String {
        engine.stop()
        defer { state = .idle }
        return transcript.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// "Cancel": nothing is kept.
    public func cancel() { engine.stop(); transcript = ""; state = .idle }
}

/// `SFSpeechRecognizer` with `requiresOnDeviceRecognition = true`: audio never leaves the phone.
@MainActor
public final class SystemSpeechTranscriber: SpeechTranscribing {
    private let recognizer: SFSpeechRecognizer?
    private let audio = AVAudioEngine()
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?

    public init(locale: Locale = .current) { recognizer = SFSpeechRecognizer(locale: locale) ?? SFSpeechRecognizer() }

    public func requestAccess() async -> SpeechAccess {
        guard let recognizer, recognizer.supportsOnDeviceRecognition else { return .unavailable }
        let speech: SFSpeechRecognizerAuthorizationStatus = await withCheckedContinuation { c in
            SFSpeechRecognizer.requestAuthorization { @Sendable in c.resume(returning: $0) }
        }
        guard speech == .authorized else { return .denied }
        let mic: Bool = await withCheckedContinuation { c in
            AVAudioApplication.requestRecordPermission { @Sendable in c.resume(returning: $0) }
        }
        return mic ? .ready : .denied
    }

    public func start(update: @escaping @MainActor (String, Double) -> Void) throws {
        guard let recognizer else { throw CocoaError(.featureUnsupported) }
        stop()
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.record, mode: .measurement, options: .duckOthers)
        try session.setActive(true, options: .notifyOthersOnDeactivation)

        let req = SFSpeechAudioBufferRecognitionRequest()
        req.shouldReportPartialResults = true
        req.requiresOnDeviceRecognition = true
        request = req

        let input = audio.inputNode
        let format = input.outputFormat(forBus: 0)
        let sink = Sink(request: req, update: update)
        input.installTap(onBus: 0, bufferSize: 1024, format: format) { @Sendable buffer, _ in sink.feed(buffer) }
        audio.prepare()
        try audio.start()

        task = recognizer.recognitionTask(with: req) { @Sendable result, _ in
            if let text = result?.bestTranscription.formattedString { sink.words(text) }
        }
    }

    public func stop() {
        if audio.isRunning { audio.stop(); audio.inputNode.removeTap(onBus: 0) }
        request?.endAudio(); request = nil
        task?.cancel(); task = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    /// Crosses from the audio thread to the main actor.
    private final class Sink: @unchecked Sendable {
        let request: SFSpeechAudioBufferRecognitionRequest
        let update: @MainActor (String, Double) -> Void
        private let lock = NSLock()
        private var text = ""
        init(request: SFSpeechAudioBufferRecognitionRequest, update: @escaping @MainActor (String, Double) -> Void) {
            self.request = request; self.update = update
        }
        func feed(_ buffer: AVAudioPCMBuffer) {
            request.append(buffer)
            var peak: Float = 0
            if let ch = buffer.floatChannelData?[0] { for i in 0..<Int(buffer.frameLength) { peak = max(peak, abs(ch[i])) } }
            let level = Double(min(1, peak * 3)), current = lock.withLock { text }
            Task { @MainActor [update] in update(current, level) }
        }
        func words(_ t: String) {
            lock.withLock { text = t }
            Task { @MainActor [update] in update(t, 0.15) }
        }
    }
}
