import Cocoa
import Foundation

// MARK: - Application Entry Point

let position = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "top-right"
let alwaysOnTop = CommandLine.arguments.count > 2 ? CommandLine.arguments[2] == "true" : true

let outputWriter = OutputWriter()
let windowCoordinator = WindowCoordinator()

let viewModel = MainViewModel(
    outputWriter: outputWriter,
    windowCoordinator: windowCoordinator
)

let dispatcher = EventDispatcher()
dispatcher.delegate = viewModel

let mainWindow = MainWindow(
    position: position,
    alwaysOnTop: alwaysOnTop,
    viewModel: viewModel
)
mainWindow.makeKeyAndOrderFront(nil)

let eventReader = EventReader(dispatcher: dispatcher)
eventReader.startReading()

signal(SIGTERM) { _ in
    NSLog("[main] Received SIGTERM, shutting down")
    windowCoordinator.closeAllOverlays()
    NSApplication.shared.terminate(nil)
}

NSApplication.shared.run()
