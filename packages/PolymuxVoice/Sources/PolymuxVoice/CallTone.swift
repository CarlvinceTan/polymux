import AVFoundation

/// The short chimes that open and close a call.
enum CallTone {
    case connect
    case end

    private var notes: [Double] {
        switch self {
        case .connect: [660, 880]
        case .end: [880, 520]
        }
    }

    /// 16-bit mono WAV, so no audio file ships with the app.
    var wav: Data {
        let rate = 44_100.0
        let length = Int(rate * 0.11)
        var samples: [Int16] = []
        for frequency in notes {
            for index in 0..<length {
                let time = Double(index) / rate
                let fade = min(1, Double(index) / 400, Double(length - index) / 1200)
                samples.append(Int16(sin(2 * .pi * frequency * time) * fade * 0.25 * 32767))
            }
        }
        var data = Data()
        func put<T: FixedWidthInteger>(_ value: T) { withUnsafeBytes(of: value.littleEndian) { data.append(contentsOf: $0) } }
        data.append(contentsOf: Array("RIFF".utf8)); put(UInt32(36 + samples.count * 2))
        data.append(contentsOf: Array("WAVEfmt ".utf8)); put(UInt32(16)); put(UInt16(1)); put(UInt16(1))
        put(UInt32(rate)); put(UInt32(rate) * 2); put(UInt16(2)); put(UInt16(16))
        data.append(contentsOf: Array("data".utf8)); put(UInt32(samples.count * 2))
        for sample in samples { put(sample) }
        return data
    }
}
