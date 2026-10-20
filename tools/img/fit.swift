import AppKit
// fit <in> <out.jpg> <size> [trim]: scale the image into a white size×size square (JPEG, quality 0.85).
// "trim" first crops away near-white margins around the product.
let a = CommandLine.arguments
guard a.count >= 4, let src = NSImage(contentsOfFile: a[1]), let N = Int(a[3]) else { print("usage: fit in out size [trim]"); exit(1) }
let bmp = NSBitmapImageRep(data: src.tiffRepresentation!)!
let w = bmp.pixelsWide, h = bmp.pixelsHigh
var crop = NSRect(x: 0, y: 0, width: w, height: h)     // in bitmap pixels, origin top-left
if a.count >= 5 && a[4] == "trim" {
  var x0 = w, y0 = h, x1 = -1, y1 = -1
  for y in stride(from: 0, to: h, by: 2) {
    for x in stride(from: 0, to: w, by: 2) {
      guard let c = bmp.colorAt(x: x, y: y)?.usingColorSpace(.deviceRGB) else { continue }
      if c.alphaComponent > 0.1 && (c.redComponent < 0.94 || c.greenComponent < 0.94 || c.blueComponent < 0.94) {
        x0 = min(x0, x); x1 = max(x1, x); y0 = min(y0, y); y1 = max(y1, y)
      }
    }
  }
  if x1 > x0 && y1 > y0 { crop = NSRect(x: x0, y: y0, width: x1 - x0 + 2, height: y1 - y0 + 2) }
}
let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: N, pixelsHigh: N, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState(); NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
NSColor.white.setFill(); NSRect(x: 0, y: 0, width: N, height: N).fill()
let k = min(Double(N) * 0.94 / crop.width, Double(N) * 0.94 / crop.height)
let dw = crop.width * k, dh = crop.height * k
let img = NSImage(size: NSSize(width: w, height: h)); img.addRepresentation(bmp)
let from = NSRect(x: crop.minX, y: Double(h) - crop.maxY, width: crop.width, height: crop.height)   // flip to bottom-left
img.draw(in: NSRect(x: (Double(N) - dw) / 2, y: (Double(N) - dh) / 2, width: dw, height: dh), from: from, operation: .sourceOver, fraction: 1)
NSGraphicsContext.restoreGraphicsState()
try! rep.representation(using: .jpeg, properties: [.compressionFactor: 0.85])!.write(to: URL(fileURLWithPath: a[2]))
