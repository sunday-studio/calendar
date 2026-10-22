import Testing
@testable import CalendarCore

@Suite struct HapticMapTests {
    @Test func everyTriggerHasAFeeling() {
        for t in HapticTrigger.allCases { _ = HapticMap.feedback(for: t) }
    }

    @Test func tableMatchesTheBrief() {
        for t in [HapticTrigger.snap, .viewSwitch, .dayPage, .monthPick, .rowTap, .today, .menuPick] { #expect(HapticMap.feedback(for: t) == .selection) }
        #expect(HapticMap.feedback(for: .primaryButton) == .impact(.light))
        #expect(HapticMap.feedback(for: .fab) == .impact(.light))
        #expect(HapticMap.feedback(for: .sheetOpen) == .impact(.soft))
        #expect(HapticMap.feedback(for: .lift) == .impact(.rigid))
        for t in [HapticTrigger.saved, .rsvpSent, .undoRestored] { #expect(HapticMap.feedback(for: t) == .notification(.success)) }
        #expect(HapticMap.feedback(for: .conflict) == .notification(.warning))
        #expect(HapticMap.feedback(for: .failure) == .notification(.error))
    }
}

@MainActor @Suite struct HapticsServiceTests {
    @Test func playsThroughTheSinkWhenEnabled() {
        let h = Haptics()
        var got: [HapticFeedback] = []
        h.sink = { got.append($0) }
        h.isEnabled = { true }
        h.play(.snap); h.play(.saved)
        #expect(got == [.selection, .notification(.success)])
    }

    @Test func silentWhenTheSettingIsOff() {
        let h = Haptics()
        var got: [HapticFeedback] = []
        h.sink = { got.append($0) }
        h.isEnabled = { false }
        h.play(.lift); h.play(.failure)
        #expect(got.isEmpty)
    }
}
