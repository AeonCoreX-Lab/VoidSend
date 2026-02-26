import Foundation

public class VoidSendSDK {
    public static let shared = VoidSendSDK()
    
    private let defaultBaseURL = "https://api.voidsend.com/v2"
    private var baseURL: String?
    private var apiKey: String?
    private var session: URLSession
    private var pendingJobs: [String: [String: Any]] = [:]
    private let maxRetries = 3
    
    // Callback storage
    private var dispatchCallbacks: [String: DispatchCompletion] = [:]
    private var templateCallbacks: [String: TemplateCompletion] = [:]
    
    private init() {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 30
        config.timeoutIntervalForResource = 300
        config.httpMaximumConnectionsPerHost = 10
        self.session = URLSession(configuration: config)
        
        // Load pending jobs
        loadPendingJobs()
    }
    
    // MARK: - Initialization
    
    public func initialize(baseURL: String, apiKey: String) {
        self.baseURL = baseURL
        self.apiKey = apiKey
        print("✅ VoidSend SDK initialized with base URL: \(baseURL)")
        
        // Process pending jobs
        processPendingJobs()
    }
    
    public func initialize(apiKey: String) {
        initialize(baseURL: defaultBaseURL, apiKey: apiKey)
    }
    
    // MARK: - Dispatch Methods
    
    public func dispatch(userId: String, action: String, data: [String: Any],
                        priority: Int = 5, completion: DispatchCompletion? = nil) {
        
        guard let apiKey = apiKey, let baseURL = baseURL else {
            completion?(.failure(VoidSendError.notInitialized))
            return
        }
        
        let jobId = UUID().uuidString
        
        if let completion = completion {
            dispatchCallbacks[jobId] = completion
        }
        
        var payload: [String: Any] = [
            "user_id": userId,
            "action": action,
            "data": data,
            "priority": priority,
            "tags": ["platform": "ios"]
        ]
        
        // Save for offline
        savePendingJob(jobId: jobId, payload: payload)
        
        // Execute
        executeDispatch(jobId: jobId, payload: payload, retryCount: 0)
    }
    
    public func dispatchBatch(jobs: [[String: Any]], completion: BatchCompletion? = nil) {
        guard let apiKey = apiKey, let baseURL = baseURL else {
            completion?(.failure(VoidSendError.notInitialized))
            return
        }
        
        var request = URLRequest(url: URL(string: "\(baseURL)/batch-dispatch")!)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        request.setValue("2.1.0", forHTTPHeaderField: "X-SDK-Version")
        
        let payload: [String: Any] = ["jobs": jobs]
        request.httpBody = try? JSONSerialization.data(withJSONObject: payload)
        
        let task = session.dataTask(with: request) { data, response, error in
            if let error = error {
                completion?(.failure(error))
                return
            }
            
            guard let data = data else {
                completion?(.failure(VoidSendError.noData))
                return
            }
            
            do {
                if let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] {
                    completion?(.success(json))
                } else {
                    completion?(.failure(VoidSendError.invalidResponse))
                }
            } catch {
                completion?(.failure(error))
            }
        }
        
        task.resume()
    }
    
    // MARK: - Template Management
    
    public func uploadTemplate(action: String, htmlContent: String, subject: String? = nil,
                              completion: TemplateCompletion? = nil) {
        guard let apiKey = apiKey, let baseURL = baseURL else {
            completion?(.failure(VoidSendError.notInitialized))
            return
        }
        
        let requestId = UUID().uuidString
        if let completion = completion {
            templateCallbacks[requestId] = completion
        }
        
        var payload: [String: Any] = [
            "action": action,
            "html_content": htmlContent
        ]
        if let subject = subject {
            payload["subject"] = subject
        }
        
        var request = URLRequest(url: URL(string: "\(baseURL)/template/upload")!)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        request.setValue("2.1.0", forHTTPHeaderField: "X-SDK-Version")
        request.httpBody = try? JSONSerialization.data(withJSONObject: payload)
        
        let task = session.dataTask(with: request) { [weak self] data, response, error in
            guard let self = self else { return }
            
            let cb = self.templateCallbacks.removeValue(forKey: requestId)
            
            if let error = error {
                cb?(.failure(error))
                return
            }
            
            guard let data = data,
                  let httpResponse = response as? HTTPURLResponse,
                  httpResponse.statusCode == 200 else {
                cb?(.failure(VoidSendError.uploadFailed))
                return
            }
            
            do {
                if let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] {
                    cb?(.success(json))
                } else {
                    cb?(.failure(VoidSendError.invalidResponse))
                }
            } catch {
                cb?(.failure(error))
            }
        }
        
        task.resume()
    }
    
    public func listTemplates(completion: @escaping (Result<[[String: Any]], Error>) -> Void) {
        guard let apiKey = apiKey, let baseURL = baseURL else {
            completion(.failure(VoidSendError.notInitialized))
            return
        }
        
        var request = URLRequest(url: URL(string: "\(baseURL)/template/list")!)
        request.httpMethod = "GET"
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        request.setValue("2.1.0", forHTTPHeaderField: "X-SDK-Version")
        
        let task = session.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }
            
            guard let data = data else {
                completion(.failure(VoidSendError.noData))
                return
            }
            
            do {
                if let json = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                   let templates = json["templates"] as? [[String: Any]] {
                    completion(.success(templates))
                } else {
                    completion(.failure(VoidSendError.invalidResponse))
                }
            } catch {
                completion(.failure(error))
            }
        }
        
        task.resume()
    }
    
    public func deleteTemplate(action: String, completion: @escaping (Result<Void, Error>) -> Void) {
        guard let apiKey = apiKey, let baseURL = baseURL else {
            completion(.failure(VoidSendError.notInitialized))
            return
        }
        
        var request = URLRequest(url: URL(string: "\(baseURL)/template/\(action)")!)
        request.httpMethod = "DELETE"
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        request.setValue("2.1.0", forHTTPHeaderField: "X-SDK-Version")
        
        let task = session.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }
            
            guard let httpResponse = response as? HTTPURLResponse,
                  httpResponse.statusCode == 200 else {
                completion(.failure(VoidSendError.deleteFailed))
                return
            }
            
            completion(.success(()))
        }
        
        task.resume()
    }
    
    public func dispatchWithHtml(userId: String, htmlContent: String, data: [String: Any],
                                completion: DispatchCompletion? = nil) {
        let action = "custom_\(Int(Date().timeIntervalSince1970 * 1000))"
        
        uploadTemplate(action: action, htmlContent: htmlContent) { [weak self] result in
            switch result {
            case .success:
                self?.dispatch(userId: userId, action: action, data: data, completion: completion)
            case .failure(let error):
                completion?(.failure(error))
            }
        }
    }
    
    // MARK: - Private Methods
    
    private func executeDispatch(jobId: String, payload: [String: Any], retryCount: Int) {
        guard let apiKey = apiKey, let baseURL = baseURL else { return }
        
        var request = URLRequest(url: URL(string: "\(baseURL)/ultra-dispatch")!)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        request.setValue("2.1.0", forHTTPHeaderField: "X-SDK-Version")
        request.httpBody = try? JSONSerialization.data(withJSONObject: payload)
        
        let task = session.dataTask(with: request) { [weak self] data, response, error in
            guard let self = self else { return }
            
            if let error = error {
                if retryCount < self.maxRetries {
                    let delay = pow(2.0, Double(retryCount))
                    DispatchQueue.global().asyncAfter(deadline: .now() + delay) {
                        self.executeDispatch(jobId: jobId, payload: payload, retryCount: retryCount + 1)
                    }
                } else {
                    let callback = self.dispatchCallbacks.removeValue(forKey: jobId)
                    callback?(.failure(error))
                }
                return
            }
            
            guard let data = data,
                  let httpResponse = response as? HTTPURLResponse,
                  httpResponse.statusCode == 200 || httpResponse.statusCode == 202 else {
                if retryCount < self.maxRetries {
                    let delay = pow(2.0, Double(retryCount))
                    DispatchQueue.global().asyncAfter(deadline: .now() + delay) {
                        self.executeDispatch(jobId: jobId, payload: payload, retryCount: retryCount + 1)
                    }
                } else {
                    let callback = self.dispatchCallbacks.removeValue(forKey: jobId)
                    callback?(.failure(VoidSendError.serverError))
                }
                return
            }
            
            do {
                if let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] {
                    self.removePendingJob(jobId: jobId)
                    let callback = self.dispatchCallbacks.removeValue(forKey: jobId)
                    callback?(.success(json))
                } else {
                    let callback = self.dispatchCallbacks.removeValue(forKey: jobId)
                    callback?(.failure(VoidSendError.invalidResponse))
                }
            } catch {
                let callback = self.dispatchCallbacks.removeValue(forKey: jobId)
                callback?(.failure(error))
            }
        }
        
        task.resume()
    }
    
    private func savePendingJob(jobId: String, payload: [String: Any]) {
        var pending = UserDefaults.standard.dictionary(forKey: "voidsend_pending_jobs") ?? [:]
        pending[jobId] = payload
        UserDefaults.standard.set(pending, forKey: "voidsend_pending_jobs")
    }
    
    private func removePendingJob(jobId: String) {
        var pending = UserDefaults.standard.dictionary(forKey: "voidsend_pending_jobs") ?? [:]
        pending.removeValue(forKey: jobId)
        UserDefaults.standard.set(pending, forKey: "voidsend_pending_jobs")
    }
    
    private func loadPendingJobs() {
        pendingJobs = UserDefaults.standard.dictionary(forKey: "voidsend_pending_jobs") as? [String: [String: Any]] ?? [:]
    }
    
    private func processPendingJobs() {
        for (jobId, payload) in pendingJobs {
            executeDispatch(jobId: jobId, payload: payload, retryCount: 0)
        }
    }
}

// MARK: - Type Aliases

public typealias DispatchCompletion = (Result<[String: Any], Error>) -> Void
public typealias BatchCompletion = (Result<[String: Any], Error>) -> Void
public typealias TemplateCompletion = (Result<[String: Any], Error>) -> Void

// MARK: - Errors

public enum VoidSendError: Error {
    case notInitialized
    case noData
    case invalidResponse
    case networkError
    case uploadFailed
    case deleteFailed
    case serverError
    
    public var localizedDescription: String {
        switch self {
        case .notInitialized:
            return "SDK not initialized. Call initialize() first."
        case .noData:
            return "No data received from server"
        case .invalidResponse:
            return "Invalid response from server"
        case .networkError:
            return "Network error occurred"
        case .uploadFailed:
            return "Template upload failed"
        case .deleteFailed:
            return "Template deletion failed"
        case .serverError:
            return "Server error"
        }
    }
}

// MARK: - SwiftUI Support

@available(iOS 13.0, macOS 10.15, *)
extension VoidSendSDK {
    public func dispatch(userId: String, action: String, data: [String: Any]) async throws -> [String: Any] {
        return try await withCheckedThrowingContinuation { continuation in
            dispatch(userId: userId, action: action, data: data) { result in
                continuation.resume(with: result)
            }
        }
    }
    
    public func uploadTemplate(action: String, htmlContent: String, subject: String? = nil) async throws -> [String: Any] {
        return try await withCheckedThrowingContinuation { continuation in
            uploadTemplate(action: action, htmlContent: htmlContent, subject: subject) { result in
                continuation.resume(with: result)
            }
        }
    }
}