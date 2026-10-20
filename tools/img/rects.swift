import AppKit
// rects <in> <out.png> <x0> <y0> <x1> <y1> <hexcolor> [...]: paint filled rectangles (top-left pixel coords),
// used to cover faces / serial numbers in product photos
let a = CommandLine.arguments
guard a.count >= 8, let src = NSImage(contentsOfFile: a[1]) else { print("usage: rects in out x0 y0 x1 y1 hex ..."); exit(1) }
var W = 0, H = 0
for r in src.representations { W = max(W, r.pixelsWide); H = max(H, r.pixelsHigh) }
let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: W, pixelsHigh: H, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState(); NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
src.draw(in: NSRect(x: 0, y: 0, width: W, height: H))
var i = 3
while i + 4 < a.count {
  let x0 = Double(a[i])!, y0 = Double(a[i+1])!, x1 = Double(a[i+2])!, y1 = Double(a[i+3])!
  let h = UInt32(a[i+4], radix: 16)!
  NSColor(red: CGFloat((h >> 16) & 255) / 255, green: CGFloat((h >> 8) & 255) / 255, blue: CGFloat(h & 255) / 255, alpha: 1).setFill()
  NSRect(x: x0, y: Double(H) - y1, width: x1 - x0, height: y1 - y0).fill()
  i += 5
}
NSGraphicsContext.restoreGraphicsState()
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: a[2]))
