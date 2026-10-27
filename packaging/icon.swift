import AppKit
let size = 1024
let image = NSImage(size: NSSize(width: size, height: size))
image.lockFocus()
NSColor(calibratedRed: 0.025, green: 0.045, blue: 0.075, alpha: 1).setFill()
NSBezierPath(roundedRect: NSRect(x: 12, y: 12, width: 1000, height: 1000), xRadius: 220, yRadius: 220).fill()
let center = NSPoint(x: 512, y: 512)
for ring in 0..<6 {
 let radius = 135.0 + Double(ring)*45
 let count = 20 + ring*8
 for i in 0..<count {
  let angle = Double(i)/Double(count)*Double.pi*2 + Double(ring)*0.16
  let r = 6.5 + Double(5-ring)*0.7
  let x = Double(center.x)+cos(angle)*radius
  let y = Double(center.y)+sin(angle)*radius
  NSColor(calibratedRed: 0.11, green: 0.45+Double(ring)*0.04, blue: 1, alpha: 1-Double(ring)*0.08).setFill()
  NSBezierPath(ovalIn: NSRect(x:x-r,y:y-r,width:r*2,height:r*2)).fill()
 }
}
let orbit = NSBezierPath()
orbit.move(to:NSPoint(x:430,y:400));orbit.line(to:NSPoint(x:580,y:400));orbit.line(to:NSPoint(x:630,y:512));orbit.line(to:NSPoint(x:555,y:624));orbit.line(to:NSPoint(x:420,y:585));orbit.line(to:NSPoint(x:390,y:475));orbit.close()
NSColor(calibratedRed:0.18,green:0.55,blue:1,alpha:1).setStroke();orbit.lineWidth=18;orbit.lineJoinStyle = .round;orbit.stroke()
image.unlockFocus()
let bitmap = NSBitmapImageRep(data: image.tiffRepresentation!)!
try bitmap.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:CommandLine.arguments[1]))
