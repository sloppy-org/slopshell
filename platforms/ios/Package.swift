// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "TaburaFlowContract",
    products: [
        .library(
            name: "TaburaFlowContract",
            targets: ["TaburaFlowContract"]
        ),
        .library(
            name: "TaburaIOSModels",
            targets: ["TaburaIOSModels"]
        ),
    ],
    targets: [
        .target(
            name: "TaburaFlowContract"
        ),
        .target(
            name: "TaburaIOSModels",
            path: "TaburaIOS",
            exclude: [
                "ContentView.swift",
                "Info.plist",
                "TaburaAppModel.swift",
                "TaburaAudioCapture.swift",
                "TaburaCanvasTransport.swift",
                "TaburaCanvasWebView.swift",
                "TaburaChatTransport.swift",
                "TaburaIOSApp.swift",
                "TaburaInkCaptureView.swift",
                "TaburaServerDiscovery.swift",
            ],
            sources: ["TaburaModels.swift"]
        ),
        .testTarget(
            name: "TaburaFlowContractTests",
            dependencies: ["TaburaFlowContract"],
            resources: [.process("Resources")]
        ),
        .testTarget(
            name: "TaburaIOSModelsTests",
            dependencies: ["TaburaIOSModels"],
            path: "Tests/TaburaIOSModelsTests"
        ),
    ]
)
