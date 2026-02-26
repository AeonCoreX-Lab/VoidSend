import 'dart:convert';
import 'dart:math';
import 'dart:async';

import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

class VoidSendSDK {
  static final VoidSendSDK _instance = VoidSendSDK._internal();
  factory VoidSendSDK() => _instance;
  VoidSendSDK._internal();

  static const String _defaultBaseUrl = 'https://api.voidsend.com/v2';
  static const int _maxRetries = 3;
  
  late String _baseUrl;
  late String _apiKey;
  late http.Client _httpClient;
  late SharedPreferences _prefs;
  Map<String, Map<String, dynamic>> _pendingJobs = {};
  bool _isInitialized = false;

  // Callback maps for different request types
  final Map<String, VoidSendCallback> _dispatchCallbacks = {};
  final Map<String, TemplateCallback> _templateCallbacks = {};

  /// Initialize SDK with custom base URL
  Future<void> initialize(String baseUrl, String apiKey) async {
    _baseUrl = baseUrl;
    _apiKey = apiKey;
    _httpClient = http.Client();
    _prefs = await SharedPreferences.getInstance();
    _isInitialized = true;
    
    // Load pending jobs
    _loadPendingJobs();
    
    // Process pending jobs
    _processPendingJobs();
    
    print('✅ VoidSend SDK initialized with base URL: $baseUrl');
  }

  /// Initialize SDK with default base URL
  Future<void> initializeWithDefault(String apiKey) async {
    await initialize(_defaultBaseUrl, apiKey);
  }

  // ==================== DISPATCH METHODS ====================

  Future<void> dispatch({
    required String userId,
    required String action,
    Map<String, dynamic>? data,
    int priority = 5,
    VoidSendCallback? callback,
  }) async {
    if (!_isInitialized) {
      callback?.onError(VoidSendError.notInitialized);
      return;
    }

    String jobId = _generateUUID();
    
    if (callback != null) {
      _dispatchCallbacks[jobId] = callback;
    }
    
    Map<String, dynamic> payload = {
      'user_id': userId,
      'action': action,
      'data': data ?? {},
      'priority': priority,
      'tags': {'platform': 'flutter'},
    };

    // Save for offline
    await _savePendingJob(jobId, payload);

    // Execute
    _executeDispatch(jobId, payload, 0);
  }

  Future<void> dispatchBatch(List<Map<String, dynamic>> jobs, 
      {VoidSendBatchCallback? callback}) async {
    if (!_isInitialized) {
      callback?.onError(VoidSendError.notInitialized);
      return;
    }

    try {
      final response = await _httpClient.post(
        Uri.parse('$_baseUrl/batch-dispatch'),
        headers: _getHeaders(),
        body: jsonEncode({'jobs': jobs}),
      );

      if (response.statusCode == 200 || response.statusCode == 202) {
        final data = jsonDecode(response.body);
        callback?.onSuccess(data);
      } else {
        callback?.onError(Exception('Server error: ${response.statusCode}'));
      }
    } catch (e) {
      callback?.onError(e);
    }
  }

  // ==================== TEMPLATE MANAGEMENT ====================

  /// Upload a custom HTML template
  Future<void> uploadTemplate({
    required String action,
    required String htmlContent,
    String? subject,
    TemplateCallback? callback,
  }) async {
    if (!_isInitialized) {
      callback?.onError(VoidSendError.notInitialized);
      return;
    }

    String requestId = _generateUUID();
    if (callback != null) {
      _templateCallbacks[requestId] = callback;
    }

    try {
      final payload = {
        'action': action,
        'html_content': htmlContent,
        if (subject != null) 'subject': subject,
      };

      final response = await _httpClient.post(
        Uri.parse('$_baseUrl/template/upload'),
        headers: _getHeaders(),
        body: jsonEncode(payload),
      );

      final cb = _templateCallbacks.remove(requestId);
      if (response.statusCode == 200) {
        final data = jsonDecode(response.body);
        cb?.onSuccess('Template uploaded successfully', data);
      } else {
        cb?.onError('Upload failed: ${response.statusCode}');
      }
    } catch (e) {
      final cb = _templateCallbacks.remove(requestId);
      cb?.onError(e.toString());
    }
  }

  /// List all templates
  Future<List<dynamic>> listTemplates() async {
    if (!_isInitialized) {
      throw VoidSendError.notInitialized;
    }

    try {
      final response = await _httpClient.get(
        Uri.parse('$_baseUrl/template/list'),
        headers: _getHeaders(),
      );

      if (response.statusCode == 200) {
        final data = jsonDecode(response.body);
        return data['templates'] ?? [];
      } else {
        throw Exception('Failed to list templates: ${response.statusCode}');
      }
    } catch (e) {
      throw Exception('List templates failed: $e');
    }
  }

  /// Delete a template
  Future<void> deleteTemplate(String action) async {
    if (!_isInitialized) {
      throw VoidSendError.notInitialized;
    }

    try {
      final response = await _httpClient.delete(
        Uri.parse('$_baseUrl/template/$action'),
        headers: _getHeaders(),
      );

      if (response.statusCode != 200) {
        throw Exception('Delete failed: ${response.statusCode}');
      }
    } catch (e) {
      throw Exception('Delete template failed: $e');
    }
  }

  /// Convenience: send email with custom HTML (auto-upload)
  Future<void> dispatchWithHtml({
    required String userId,
    required String htmlContent,
    Map<String, dynamic>? data,
    VoidSendCallback? callback,
  }) async {
    String action = 'custom_${DateTime.now().millisecondsSinceEpoch}';
    
    // First upload template
    await uploadTemplate(
      action: action,
      htmlContent: htmlContent,
      callback: TemplateCallback(
        onSuccess: (message, response) {
          // Then dispatch
          dispatch(
            userId: userId,
            action: action,
            data: data,
            callback: callback,
          );
        },
        onError: (error) {
          callback?.onError(Exception('Template upload failed: $error'));
        },
      ),
    );
  }

  // ==================== INTERNAL METHODS ====================

  void _executeDispatch(String jobId, Map<String, dynamic> payload, 
      int retryCount) async {
    try {
      final response = await _httpClient.post(
        Uri.parse('$_baseUrl/ultra-dispatch'),
        headers: _getHeaders(),
        body: jsonEncode(payload),
      );

      if (response.statusCode == 200 || response.statusCode == 202) {
        await _removePendingJob(jobId);
        final data = jsonDecode(response.body);
        final callback = _dispatchCallbacks.remove(jobId);
        callback?.onSuccess(data);
      } else if (retryCount < _maxRetries) {
        final delay = pow(2, retryCount) * 1000;
        await Future.delayed(Duration(milliseconds: delay.toInt()));
        _executeDispatch(jobId, payload, retryCount + 1);
      } else {
        final callback = _dispatchCallbacks.remove(jobId);
        callback?.onError(Exception('Max retries reached: ${response.statusCode}'));
      }
    } catch (e) {
      if (retryCount < _maxRetries) {
        final delay = pow(2, retryCount) * 1000;
        await Future.delayed(Duration(milliseconds: delay.toInt()));
        _executeDispatch(jobId, payload, retryCount + 1);
      } else {
        final callback = _dispatchCallbacks.remove(jobId);
        callback?.onError(e);
      }
    }
  }

  Map<String, String> _getHeaders() {
    return {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer $_apiKey',
      'X-SDK-Version': '2.1.0',
      'X-SDK-Platform': 'flutter',
    };
  }

  Future<void> _savePendingJob(String jobId, Map<String, dynamic> payload) async {
    _pendingJobs[jobId] = payload;
    await _prefs.setString('pending_job_$jobId', jsonEncode(payload));
  }

  Future<void> _removePendingJob(String jobId) async {
    _pendingJobs.remove(jobId);
    await _prefs.remove('pending_job_$jobId');
  }

  void _loadPendingJobs() {
    final keys = _prefs.getKeys().where((key) => key.startsWith('pending_job_'));
    for (var key in keys) {
      final jobId = key.substring(12);
      final payloadStr = _prefs.getString(key);
      if (payloadStr != null) {
        try {
          _pendingJobs[jobId] = jsonDecode(payloadStr);
        } catch (e) {
          print('Error loading pending job: $e');
        }
      }
    }
  }

  void _processPendingJobs() {
    _pendingJobs.forEach((jobId, payload) {
      _executeDispatch(jobId, payload, 0);
    });
  }

  String _generateUUID() {
    final random = Random.secure();
    final bytes = List<int>.generate(16, (_) => random.nextInt(256));
    return '${_bytesToHex(bytes.sublist(0, 4))}-'
        '${_bytesToHex(bytes.sublist(4, 6))}-'
        '${_bytesToHex(bytes.sublist(6, 8))}-'
        '${_bytesToHex(bytes.sublist(8, 10))}-'
        '${_bytesToHex(bytes.sublist(10, 16))}';
  }

  String _bytesToHex(List<int> bytes) {
    return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }
}

// ==================== CALLBACK CLASSES ====================

class VoidSendCallback {
  final void Function(Map<String, dynamic> response) onSuccess;
  final void Function(dynamic error) onError;

  VoidSendCallback({required this.onSuccess, required this.onError});
}

class VoidSendBatchCallback {
  final void Function(Map<String, dynamic> response) onSuccess;
  final void Function(dynamic error) onError;

  VoidSendBatchCallback({required this.onSuccess, required this.onError});
}

class TemplateCallback {
  final void Function(String message, Map<String, dynamic> response) onSuccess;
  final void Function(String error) onError;

  TemplateCallback({required this.onSuccess, required this.onError});
}

// ==================== ERROR CLASS ====================

class VoidSendError implements Exception {
  final String message;
  static const notInitialized = VoidSendError('SDK not initialized');
  
  const VoidSendError(this.message);
  
  @override
  String toString() => 'VoidSendError: $message';
}