#!/usr/bin/env swift
// Generates the app icon (light / dark / tinted) and the launch wordmark from the flip-cell spec in
// docs/specs/app-icon-launch.md. Deterministic. Run from the repo root:  swift scripts/make-icons.swift
import AppKit
import CoreText
import ImageIO
import UniformTypeIdentifiers

let root = FileManager.default.currentDirectoryPath
let fontURL = URL(fileURLWithPath: "\(root)/ios/Calendar/Resources/Fonts/DMMono-Medium.ttf")
CTFontManagerRegisterFontsForURL(fontURL as CFURL, .process, nil)
let assets = "\(root)/ios/Calendar/Resources/Assets.xcassets"

func rgb(_ hex: UInt32, _ a: CGFloat = 1) -> CGColor {
    CGColor(srgbRed: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255, blue: CGFloat(hex & 0xFF) / 255, alpha: a)
}

func png(_ w: Int, _ h: Int, opaque: Bool, _ draw: (CGContext) -> Void, to path: String) {
    let cs = CGColorSpace(name: CGColorSpace.sRGB)!
    let info = opaque ? CGImageAlphaInfo.noneSkipLast.rawValue : CGImageAlphaInfo.premultipliedLast.rawValue
    let ctx = CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0, space: cs, bitmapInfo: info)!
    ctx.translateBy(x: 0, y: CGFloat(h)); ctx.scaleBy(x: 1, y: -1)   // top-left origin
    draw(ctx)
    let dest = CGImageDestinationCreateWithURL(URL(fileURLWithPath: path) as CFURL, UTType.png.identifier as CFString, 1, nil)!
    CGImageDestinationAddImage(dest, ctx.makeImage()!, nil)
    CGImageDestinationFinalize(dest)
}

func glyph(_ ctx: CGContext, _ ch: String, size: CGFloat, in rect: CGRect, color: CGColor) {
    let font = CTFontCreateWithName("DMMono-Medium" as CFString, size, nil)
    let attrs: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: color]
    let line = CTLineCreateWithAttributedString(NSAttributedString(string: ch, attributes: attrs))
    let b = CTLineGetBoundsWithOptions(line, .useGlyphPathBounds)
    ctx.saveGState()
    ctx.textMatrix = CGAffineTransform(scaleX: 1, y: -1)             // our context is flipped
    ctx.textPosition = CGPoint(x: rect.midX - b.width / 2 - b.minX, y: rect.midY + b.height / 2 + b.minY)
    CTLineDraw(line, ctx)
    ctx.restoreGState()
}

struct Icon { let name: String; let bg: UInt32; let cell: UInt32; let ink: UInt32; let split: UInt32; let splitAlpha: CGFloat; let cellAlpha: CGFloat }
let icons = [
    Icon(name: "AppIcon-light", bg: 0xF1F1EE, cell: 0x0A0A0B, ink: 0xF4F1EA, split: 0x8A5A00, splitAlpha: 1, cellAlpha: 1),
    Icon(name: "AppIcon-dark", bg: 0x0A0A0B, cell: 0x26262A, ink: 0xF4F1EA, split: 0xF0B84A, splitAlpha: 1, cellAlpha: 1),
    Icon(name: "AppIcon-tinted", bg: 0x000000, cell: 0xFFFFFF, ink: 0x000000, split: 0xFFFFFF, splitAlpha: 0.4, cellAlpha: 0.85),
]
let text = Array("10:30")
for ic in icons {
    png(1024, 1024, opaque: true, { ctx in
        ctx.setFillColor(rgb(ic.bg)); ctx.fill(CGRect(x: 0, y: 0, width: 1024, height: 1024))
        let cw: CGFloat = 148, ch: CGFloat = 420, gap: CGFloat = 14
        let total = CGFloat(text.count) * cw + CGFloat(text.count - 1) * gap
        let x0 = (1024 - total) / 2, y0 = (1024 - ch) / 2
        for (i, c) in text.enumerated() {
            let r = CGRect(x: x0 + CGFloat(i) * (cw + gap), y: y0, width: cw, height: ch)
            ctx.setFillColor(rgb(ic.cell, ic.cellAlpha)); ctx.fill(r)
            ctx.setFillColor(rgb(ic.split, ic.splitAlpha)); ctx.fill(CGRect(x: r.minX, y: r.midY - 5, width: cw, height: 10))
            glyph(ctx, String(c), size: 215, in: r, color: rgb(ic.ink))
        }
    }, to: "\(assets)/AppIcon.appiconset/\(ic.name).png")
}

// Dev icon (Debug builds only): amber ground and a DEV band, so it never reads as the production app on a home screen.
let devSet = "\(assets)/AppIcon-Dev.appiconset"
try? FileManager.default.createDirectory(atPath: devSet, withIntermediateDirectories: true)
png(1024, 1024, opaque: true, { ctx in
    ctx.setFillColor(rgb(0xF0B84A)); ctx.fill(CGRect(x: 0, y: 0, width: 1024, height: 1024))
    let cw: CGFloat = 148, ch: CGFloat = 340, gap: CGFloat = 14
    let total = CGFloat(text.count) * cw + CGFloat(text.count - 1) * gap
    let x0 = (1024 - total) / 2, y0: CGFloat = 150
    for (i, c) in text.enumerated() {
        let r = CGRect(x: x0 + CGFloat(i) * (cw + gap), y: y0, width: cw, height: ch)
        ctx.setFillColor(rgb(0x0A0A0B)); ctx.fill(r)
        ctx.setFillColor(rgb(0xF0B84A)); ctx.fill(CGRect(x: r.minX, y: r.midY - 5, width: cw, height: 10))
        glyph(ctx, String(c), size: 190, in: r, color: rgb(0xF4F1EA))
    }
    let band = CGRect(x: 0, y: 700, width: 1024, height: 190)
    ctx.setFillColor(rgb(0x0A0A0B)); ctx.fill(band)
    glyph(ctx, "DEV", size: 150, in: band, color: rgb(0xF0B84A))
}, to: "\(devSet)/AppIcon-dev.png")
try? """
{
  "images" : [
    { "filename" : "AppIcon-dev.png", "idiom" : "universal", "platform" : "ios", "size" : "1024x1024" }
  ],
  "info" : { "author" : "xcode", "version" : 1 }
}

""".write(toFile: "\(devSet)/Contents.json", atomically: true, encoding: .utf8)

// Launch wordmark: CALENDAR, xl cells 26.4 x 60 pt (glyph 44), 2pt gap included in the pitch.
for scale in [2, 3] {
    let s = CGFloat(scale)
    let word = Array("CALENDAR"), pitch: CGFloat = 26.4, h: CGFloat = 60
    let wPt = pitch * CGFloat(word.count), wPx = Int((wPt * s).rounded(.up)), hPx = Int(h * s)
    png(wPx, hPx, opaque: false, { ctx in
        ctx.scaleBy(x: s, y: s)
        for (i, c) in word.enumerated() {
            let r = CGRect(x: CGFloat(i) * pitch, y: 0, width: pitch - 2, height: h)
            ctx.setFillColor(rgb(0x26262A)); ctx.fill(r)
            glyph(ctx, String(c), size: 44, in: r, color: rgb(0xF4F1EA))
            ctx.setFillColor(rgb(0x050505)); ctx.fill(CGRect(x: r.minX, y: r.midY - 1, width: r.width, height: 2))
        }
    }, to: "\(assets)/LaunchMark.imageset/LaunchMark@\(scale)x.png")
}
print("ok")
