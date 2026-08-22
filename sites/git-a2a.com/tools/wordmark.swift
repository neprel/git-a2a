import CoreGraphics
import CoreText
import Foundation

func font(at path: String, size: CGFloat) -> CTFont {
    guard let provider = CGDataProvider(url: URL(fileURLWithPath: path) as CFURL),
          let graphicsFont = CGFont(provider) else {
        fatalError("cannot load font at \(path)")
    }
    return CTFontCreateWithGraphicsFont(graphicsFont, size, nil, nil)
}

func number(_ value: CGFloat) -> String {
    String(format: "%.3f", Double(value)).replacingOccurrences(of: #"\.?0+$"#, with: "", options: .regularExpression)
}

func pathData(_ path: CGPath) -> String {
    var result = ""
    path.applyWithBlock { pointer in
        let element = pointer.pointee
        switch element.type {
        case .moveToPoint:
            result += "M\(number(element.points[0].x)) \(number(element.points[0].y))"
        case .addLineToPoint:
            result += "L\(number(element.points[0].x)) \(number(element.points[0].y))"
        case .addQuadCurveToPoint:
            result += "Q\(number(element.points[0].x)) \(number(element.points[0].y)) \(number(element.points[1].x)) \(number(element.points[1].y))"
        case .addCurveToPoint:
            result += "C\(number(element.points[0].x)) \(number(element.points[0].y)) \(number(element.points[1].x)) \(number(element.points[1].y)) \(number(element.points[2].x)) \(number(element.points[2].y))"
        case .closeSubpath:
            result += "Z"
        @unknown default:
            fatalError("unknown path element")
        }
    }
    return result
}

func outlined(_ text: String, font: CTFont, x: CGFloat, fill: String) -> (String, CGFloat) {
    let utf16 = Array(text.utf16)
    var glyphs = Array(repeating: CGGlyph(), count: utf16.count)
    var characters = utf16
    guard CTFontGetGlyphsForCharacters(font, &characters, &glyphs, glyphs.count) else {
        fatalError("font does not contain \(text)")
    }
    var cursor = x
    var output = "<g fill=\"\(fill)\" transform=\"translate(0 16.4) scale(1 -1)\">"
    for glyph in glyphs {
        if let path = CTFontCreatePathForGlyph(font, glyph, nil) {
            output += "<path transform=\"translate(\(number(cursor)) 0)\" d=\"\(pathData(path))\"/>"
        }
        var current = glyph
        cursor += CTFontGetAdvancesForGlyphs(font, .horizontal, &current, nil, 1)
    }
    output += "</g>"
    return (output, cursor)
}

let tools = URL(fileURLWithPath: #filePath).deletingLastPathComponent().path
let output = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : tools + "/../assets/wordmark.svg"
let regular = font(at: tools + "/JetBrainsMono-Regular.ttf", size: 15)
let bold = font(at: tools + "/JetBrainsMono-Bold.ttf", size: 15)
let first = outlined("git-", font: regular, x: 29, fill: "#7a7a72")
let second = outlined("a2a", font: bold, x: first.1 - 0.6, fill: "#111110")
let width = ceil(second.1 + 1)
let svg = """
<svg xmlns="http://www.w3.org/2000/svg" width="\(Int(width))" height="20" viewBox="0 0 \(Int(width)) 20" fill="none">
  <path d="M3.5 6.5h5.2M3.5 13.5h5.2M11.3 10h5.2" stroke="#cdcdc5" stroke-width="1.4"/>
  <path d="M8.7 6.5 11.3 10 8.7 13.5" stroke="#0a6c81" stroke-width="1.4"/>
  <circle cx="3" cy="6.5" r="2" fill="#111110"/>
  <circle cx="3" cy="13.5" r="2" fill="#111110"/>
  <circle cx="17" cy="10" r="2" fill="#0a6c81"/>
  \(first.0)
  \(second.0)
</svg>
"""
try svg.write(toFile: output, atomically: true, encoding: .utf8)
print("wrote \(output)")
