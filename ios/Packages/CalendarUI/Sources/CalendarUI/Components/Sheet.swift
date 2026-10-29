import SwiftUI
import UIKit

/// Radio mark (canvas `.rd`): 20pt circle, 1pt border; on = 6pt ink ring.
public struct RadioMark: View {
    let isOn: Bool
    public init(isOn: Bool) { self.isOn = isOn }
    public var body: some View {
        Circle().strokeBorder(isOn ? Color.calInk : Color.calControl, lineWidth: isOn ? 6 : 1)
            .frame(width: 20, height: 20).accessibilityHidden(true)
    }
}

/// Sheet list row (canvas `.li`): min h56, 19pt, hairline on top; optional right detail, radio or destructive tint.
public struct SheetRow: View {
    let title: String
    let detail: String?
    let radio: Bool?
    let isDestructive: Bool
    let isFirst: Bool
    let action: () -> Void

    public init(_ title: String, detail: String? = nil, radio: Bool? = nil, isDestructive: Bool = false,
                isFirst: Bool = false, action: @escaping () -> Void) {
        self.title = title; self.detail = detail; self.radio = radio; self.isDestructive = isDestructive
        self.isFirst = isFirst; self.action = action
    }

    public var body: some View {
        Button(action: action) {
            HStack(spacing: Spacing.m) {
                Text(title).font(CalFont.heading).foregroundStyle(isDestructive ? Color.calRed : Color.calInk)
                    .lineLimit(1).truncationMode(.tail)   // long names (calendars) never wrap
                Spacer(minLength: Spacing.s)
                if let detail { Text(detail).font(CalFont.sub).foregroundStyle(Color.calInk2) }
                if let radio { RadioMark(isOn: radio) }
            }
            .padding(.horizontal, Spacing.gutter)
            .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
            .overlay(alignment: .top) { if !isFirst { Rectangle().fill(Color.calLine).frame(height: 1) } }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(radio == true ? .isSelected : [])
    }
}

/// Bottom sheet panel (canvas `.sh`): surface, 1pt border on top, grip 32×3, optional header and sub.
public struct CalSheet<Content: View>: View {
    let title: String?
    let message: String?
    let content: Content

    public init(title: String? = nil, message: String? = nil, @ViewBuilder content: () -> Content) {
        self.title = title; self.message = message; self.content = content()
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Rectangle().fill(Color.calBorder).frame(width: 32, height: 3)
                .frame(maxWidth: .infinity).padding(.bottom, 14)
            if let title {
                Text(title).font(CalFont.font(.sansMedium, size: 20, relativeTo: .title3)).foregroundStyle(Color.calInk)
                    .padding(.horizontal, Spacing.gutter).padding(.bottom, message == nil ? 12 : 6)
                    .accessibilityAddTraits(.isHeader)
            }
            if let message {
                Text(message).font(CalFont.sub).foregroundStyle(Color.calInk2).lineSpacing(2)
                    .padding(.horizontal, Spacing.gutter).padding(.bottom, 12)
            }
            content
        }
        .padding(.top, 12).padding(.bottom, 20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.calSurface.ignoresSafeArea(edges: .bottom))
        .overlay(alignment: .top) { Rectangle().fill(Color.calBorder).frame(height: 1) }
    }
}

private struct CalSheetModifier<SheetContent: View>: ViewModifier {
    @Binding var isPresented: Bool
    let sheet: SheetContent
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content.overlay {
            ZStack(alignment: .bottom) {
                if isPresented {
                    Color(uiColor: UIColor { t in
                        t.userInterfaceStyle == .dark ? UIColor.black.withAlphaComponent(Theme.scrimAlpha.dark)
                            : UIColor(hex: Theme.scrim.light, alpha: Theme.scrimAlpha.light)
                    })
                    .ignoresSafeArea()
                    .onTapGesture { isPresented = false }
                    .transition(.opacity)
                    .accessibilityHidden(true)
                    sheet
                        .transition(Motion.transition(reduceMotion: reduceMotion, edge: .bottom))
                        .accessibilityAddTraits(.isModal)
                }
            }
            .animation(Motion.animation(reduceMotion: reduceMotion), value: isPresented)
            // A sheet opened while a field has focus would sit half behind the keyboard: drop the keyboard first.
            .onChange(of: isPresented) { _, on in
                if on { UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil) }
            }
        }
    }
}

public extension View {
    /// Present a canvas bottom sheet (scrim + `CalSheet`) over this view. Dismiss by scrim tap or by setting the binding.
    func calSheet<Sheet: View>(isPresented: Binding<Bool>, @ViewBuilder _ sheet: () -> Sheet) -> some View {
        modifier(CalSheetModifier(isPresented: isPresented, sheet: sheet()))
    }
}
