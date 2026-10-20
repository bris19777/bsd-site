import AppKit
// grid <out.png> <files...>: numbered contact sheet, 8 columns, 150px tiles
let a = Array(CommandLine.arguments.dropFirst())
let out = a[0]; let files = Array(a.dropFirst())
let T = 150, cols = 8, rows = max(1, (files.count + cols - 1) / cols)
let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: T*cols, pixelsHigh: T*rows, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState(); NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
NSColor.white.setFill(); NSRect(x: 0, y: 0, width: T*cols, height: T*rows).fill()
for (i, f) in files.enumerated() {
  guard let im = NSImage(contentsOfFile: f) else { continue }
  let c = i % cols, r = rows - 1 - i / cols
  let s = im.size; let k = CGFloat(T-6) / max(s.width, s.height)
  let w = s.width*k, h = s.height*k
  im.draw(in: NSRect(x: CGFloat(c*T) + (CGFloat(T)-w)/2, y: CGFloat(r*T) + (CGFloat(T)-h)/2, width: w, height: h))
  ("\(i+1)" as NSString).draw(at: NSPoint(x: c*T+3, y: r*T+3), withAttributes: [.font: NSFont.boldSystemFont(ofSize: 15), .foregroundColor: NSColor.red])
}
NSGraphicsContext.restoreGraphicsState()
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
