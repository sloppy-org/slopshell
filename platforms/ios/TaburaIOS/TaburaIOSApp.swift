import SwiftUI

@main
struct TaburaIOSApp: App {
    var body: some Scene {
        WindowGroup {
            if ProcessInfo.processInfo.arguments.contains("-TaburaFlowHarness") {
                TaburaFlowHarnessRootView(
                    preconditions: parseTaburaFlowHarnessPreconditions(
                        ProcessInfo.processInfo.environment["TABURA_FLOW_PRECONDITIONS_JSON"]
                    )
                )
            } else {
                ContentView()
            }
        }
    }
}
