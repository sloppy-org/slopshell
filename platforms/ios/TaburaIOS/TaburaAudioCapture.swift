import AVFoundation
import Foundation

final class TaburaAudioCapture {
    private let engine = AVAudioEngine()
    private let outputFormat: AVAudioFormat
    private let voiceThreshold: Float = 0.012
    private let onChunk: @MainActor (Data) -> Void
    private let onStateChange: @MainActor (Bool, String) -> Void
    private var isRunning = false

    init(onChunk: @escaping @MainActor (Data) -> Void, onStateChange: @escaping @MainActor (Bool, String) -> Void) {
        guard let outputFormat = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 16_000, channels: 1, interleaved: true) else {
            fatalError("failed to create capture output format")
        }
        self.outputFormat = outputFormat
        self.onChunk = onChunk
        self.onStateChange = onStateChange
    }

    func start() throws {
        if isRunning {
            return
        }
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .spokenAudio, options: [.mixWithOthers, .defaultToSpeaker, .allowBluetoothHFP])
        try session.setPreferredSampleRate(16_000)
        try session.setActive(true, options: [])

        let input = engine.inputNode
        let inputFormat = input.inputFormat(forBus: 0)
        input.removeTap(onBus: 0)
        input.installTap(onBus: 0, bufferSize: 2048, format: inputFormat) { [weak self] buffer, _ in
            guard let self else { return }
            guard self.voiceDetected(in: buffer) else { return }
            guard let converted = self.convertToPCM16(buffer: buffer) else { return }
            Task { @MainActor in
                self.onChunk(converted)
            }
        }

        engine.prepare()
        try engine.start()
        isRunning = true
        Task { @MainActor in
            self.onStateChange(true, "")
        }
    }

    func stop() {
        if !isRunning {
            return
        }
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
        try? AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation])
        isRunning = false
        Task { @MainActor in
            self.onStateChange(false, "")
        }
    }

    private func voiceDetected(in buffer: AVAudioPCMBuffer) -> Bool {
        guard let channelData = buffer.floatChannelData else {
            return true
        }
        let frameCount = Int(buffer.frameLength)
        if frameCount == 0 {
            return false
        }
        let samples = channelData[0]
        var total: Float = 0
        for index in 0..<frameCount {
            total += abs(samples[index])
        }
        let average = total / Float(frameCount)
        return average >= voiceThreshold
    }

    private func convertToPCM16(buffer: AVAudioPCMBuffer) -> Data? {
        let converter = AVAudioConverter(from: buffer.format, to: outputFormat)
        let capacity = AVAudioFrameCount(Double(buffer.frameLength) * outputFormat.sampleRate / buffer.format.sampleRate) + 512
        guard
            let converter,
            let outputBuffer = AVAudioPCMBuffer(pcmFormat: outputFormat, frameCapacity: capacity)
        else {
            return nil
        }

        var error: NSError?
        var sourceConsumed = false
        converter.convert(to: outputBuffer, error: &error) { _, outStatus in
            if sourceConsumed {
                outStatus.pointee = AVAudioConverterInputStatus.endOfStream
                return nil
            }
            sourceConsumed = true
            outStatus.pointee = AVAudioConverterInputStatus.haveData
            return buffer
        }
        if error != nil {
            return nil
        }
        guard let data = outputBuffer.int16ChannelData else {
            return nil
        }
        let sampleCount = Int(outputBuffer.frameLength)
        return Data(bytes: data[0], count: sampleCount * MemoryLayout<Int16>.size)
    }
}
