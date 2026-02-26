package com.aeoncorex.voidsend;

import android.content.Context;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;

import org.json.JSONObject;
import org.json.JSONArray;

import java.io.IOException;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

import okhttp3.*;
import android.content.SharedPreferences;

public class VoidSendSDK {
    private static final String TAG = "VoidSendSDK";
    private static final String DEFAULT_BASE_URL = "https://api.voidsend.com/v2";
    private static final int MAX_RETRIES = 3;
    private static final long RETRY_DELAY_MS = 1000;
    
    private static VoidSendSDK instance;
    private Context context;
    private String baseUrl;
    private String apiKey;
    private OkHttpClient httpClient;
    private ExecutorService executor;
    private Handler mainHandler;
    private SharedPreferences prefs;
    
    private Map<String, DispatchCallback> dispatchCallbacks;
    private Map<String, TemplateCallback> templateCallbacks;
    private boolean isInitialized = false;
    
    private VoidSendSDK() {
        this.httpClient = new OkHttpClient.Builder()
            .connectTimeout(30, java.util.concurrent.TimeUnit.SECONDS)
            .writeTimeout(30, java.util.concurrent.TimeUnit.SECONDS)
            .readTimeout(30, java.util.concurrent.TimeUnit.SECONDS)
            .build();
        this.executor = Executors.newFixedThreadPool(4);
        this.mainHandler = new Handler(Looper.getMainLooper());
        this.dispatchCallbacks = new HashMap<>();
        this.templateCallbacks = new HashMap<>();
    }
    
    public static synchronized VoidSendSDK getInstance() {
        if (instance == null) {
            instance = new VoidSendSDK();
        }
        return instance;
    }
    
    /**
     * Initialize SDK with custom base URL
     * @param context Application context
     * @param baseUrl Your VoidSend engine URL (e.g., https://your-app.onrender.com/v2)
     * @param apiKey Your API key
     */
    public void initialize(Context context, String baseUrl, String apiKey) {
        this.context = context.getApplicationContext();
        this.baseUrl = baseUrl;
        this.apiKey = apiKey;
        this.prefs = context.getSharedPreferences("voidsend_prefs", Context.MODE_PRIVATE);
        this.isInitialized = true;
        
        Log.i(TAG, "VoidSend SDK initialized with base URL: " + baseUrl);
        
        // Process any pending jobs from previous sessions
        processPendingJobs();
    }
    
    /**
     * Initialize SDK with default base URL (for VoidSend cloud)
     */
    public void initialize(Context context, String apiKey) {
        initialize(context, DEFAULT_BASE_URL, apiKey);
    }
    
    // ==================== DISPATCH METHODS ====================
    
    public void dispatch(String userId, String action, JSONObject data) {
        dispatch(userId, action, data, null);
    }
    
    public void dispatch(String userId, String action, JSONObject data, DispatchCallback callback) {
        if (!isInitialized) {
            Log.e(TAG, "SDK not initialized. Call initialize() first");
            if (callback != null) {
                callback.onError(new Exception("SDK not initialized"));
            }
            return;
        }
        
        String jobId = UUID.randomUUID().toString();
        
        if (callback != null) {
            dispatchCallbacks.put(jobId, callback);
        }
        
        JSONObject payload = new JSONObject();
        try {
            payload.put("user_id", userId);
            payload.put("action", action);
            payload.put("data", data);
            payload.put("priority", 5);
            payload.put("tags", new JSONObject().put("platform", "android"));
        } catch (Exception e) {
            Log.e(TAG, "Error creating payload", e);
            if (callback != null) {
                callback.onError(e);
            }
            return;
        }
        
        // Save for offline support
        savePendingJob(jobId, payload);
        
        // Execute network request
        executeDispatch(jobId, payload, 0);
    }
    
    public void dispatchBatch(java.util.List<DispatchRequest> requests, BatchCallback callback) {
        if (!isInitialized) {
            Log.e(TAG, "SDK not initialized");
            if (callback != null) callback.onError(new Exception("SDK not initialized"));
            return;
        }
        
        executor.execute(() -> {
            try {
                JSONArray jobsArray = new JSONArray();
                for (DispatchRequest req : requests) {
                    JSONObject job = new JSONObject();
                    job.put("user_id", req.userId);
                    job.put("action", req.action);
                    job.put("data", req.data);
                    job.put("priority", req.priority != null ? req.priority : 5);
                    jobsArray.put(job);
                }
                
                JSONObject payload = new JSONObject();
                payload.put("jobs", jobsArray);
                
                Request request = new Request.Builder()
                    .url(baseUrl + "/batch-dispatch")
                    .addHeader("Authorization", "Bearer " + apiKey)
                    .addHeader("Content-Type", "application/json")
                    .addHeader("X-SDK-Version", "2.1.0")
                    .post(RequestBody.create(
                        MediaType.parse("application/json"),
                        payload.toString()
                    ))
                    .build();
                
                try (Response response = httpClient.newCall(request).execute()) {
                    if (response.isSuccessful() && response.body() != null) {
                        String responseBody = response.body().string();
                        JSONObject json = new JSONObject(responseBody);
                        
                        if (callback != null) {
                            mainHandler.post(() -> callback.onSuccess(json));
                        }
                    } else {
                        if (callback != null) {
                            mainHandler.post(() -> callback.onError(
                                new Exception("Server error: " + response.code())
                            ));
                        }
                    }
                }
            } catch (Exception e) {
                Log.e(TAG, "Batch dispatch failed", e);
                if (callback != null) {
                    mainHandler.post(() -> callback.onError(e));
                }
            }
        });
    }
    
    // ==================== TEMPLATE MANAGEMENT ====================
    
    /**
     * Upload a custom HTML template
     * @param action The action name (e.g., "welcome-custom")
     * @param htmlContent The HTML template content
     * @param subject Optional email subject
     * @param callback Callback with result
     */
    public void uploadTemplate(String action, String htmlContent, String subject, TemplateCallback callback) {
        if (!isInitialized) {
            if (callback != null) callback.onError("SDK not initialized");
            return;
        }
        
        String requestId = UUID.randomUUID().toString();
        if (callback != null) {
            templateCallbacks.put(requestId, callback);
        }
        
        executor.execute(() -> {
            try {
                JSONObject payload = new JSONObject();
                payload.put("action", action);
                payload.put("html_content", htmlContent);
                if (subject != null && !subject.isEmpty()) {
                    payload.put("subject", subject);
                }
                
                Request request = new Request.Builder()
                    .url(baseUrl + "/template/upload")
                    .addHeader("Authorization", "Bearer " + apiKey)
                    .addHeader("Content-Type", "application/json")
                    .addHeader("X-SDK-Version", "2.1.0")
                    .post(RequestBody.create(
                        MediaType.parse("application/json"),
                        payload.toString()
                    ))
                    .build();
                
                try (Response response = httpClient.newCall(request).execute()) {
                    String responseBody = response.body() != null ? response.body().string() : "";
                    TemplateCallback cb = templateCallbacks.remove(requestId);
                    
                    if (response.isSuccessful()) {
                        JSONObject json = new JSONObject(responseBody);
                        if (cb != null) {
                            mainHandler.post(() -> cb.onSuccess("Template uploaded successfully", json));
                        }
                    } else {
                        if (cb != null) {
                            mainHandler.post(() -> cb.onError("Upload failed: " + response.code()));
                        }
                    }
                }
            } catch (Exception e) {
                Log.e(TAG, "Template upload failed", e);
                TemplateCallback cb = templateCallbacks.remove(requestId);
                if (cb != null) {
                    mainHandler.post(() -> cb.onError(e.getMessage()));
                }
            }
        });
    }
    
    /**
     * Upload template with default subject
     */
    public void uploadTemplate(String action, String htmlContent, TemplateCallback callback) {
        uploadTemplate(action, htmlContent, null, callback);
    }
    
    /**
     * List all templates for this developer
     */
    public void listTemplates(TemplateListCallback callback) {
        if (!isInitialized) {
            if (callback != null) callback.onError("SDK not initialized");
            return;
        }
        
        executor.execute(() -> {
            try {
                Request request = new Request.Builder()
                    .url(baseUrl + "/template/list")
                    .addHeader("Authorization", "Bearer " + apiKey)
                    .addHeader("Content-Type", "application/json")
                    .addHeader("X-SDK-Version", "2.1.0")
                    .get()
                    .build();
                
                try (Response response = httpClient.newCall(request).execute()) {
                    if (response.isSuccessful() && response.body() != null) {
                        String responseBody = response.body().string();
                        JSONObject json = new JSONObject(responseBody);
                        JSONArray templates = json.getJSONArray("templates");
                        if (callback != null) {
                            mainHandler.post(() -> callback.onSuccess(templates));
                        }
                    } else {
                        if (callback != null) {
                            mainHandler.post(() -> callback.onError("Failed to list templates"));
                        }
                    }
                }
            } catch (Exception e) {
                Log.e(TAG, "List templates failed", e);
                if (callback != null) {
                    mainHandler.post(() -> callback.onError(e.getMessage()));
                }
            }
        });
    }
    
    /**
     * Delete a template
     */
    public void deleteTemplate(String action, SimpleCallback callback) {
        if (!isInitialized) {
            if (callback != null) callback.onError("SDK not initialized");
            return;
        }
        
        executor.execute(() -> {
            try {
                Request request = new Request.Builder()
                    .url(baseUrl + "/template/" + action)
                    .addHeader("Authorization", "Bearer " + apiKey)
                    .addHeader("Content-Type", "application/json")
                    .addHeader("X-SDK-Version", "2.1.0")
                    .delete()
                    .build();
                
                try (Response response = httpClient.newCall(request).execute()) {
                    if (response.isSuccessful()) {
                        if (callback != null) {
                            mainHandler.post(() -> callback.onSuccess());
                        }
                    } else {
                        if (callback != null) {
                            mainHandler.post(() -> callback.onError("Delete failed: " + response.code()));
                        }
                    }
                }
            } catch (Exception e) {
                Log.e(TAG, "Delete template failed", e);
                if (callback != null) {
                    mainHandler.post(() -> callback.onError(e.getMessage()));
                }
            }
        });
    }
    
    /**
     * Convenience method: send email with custom HTML (auto-upload)
     */
    public void dispatchWithHtml(String userId, String htmlContent, JSONObject data, DispatchCallback callback) {
        String action = "custom_" + System.currentTimeMillis();
        uploadTemplate(action, htmlContent, null, new TemplateCallback() {
            @Override
            public void onSuccess(String message, JSONObject response) {
                dispatch(userId, action, data, callback);
            }
            
            @Override
            public void onError(String error) {
                if (callback != null) {
                    callback.onError(new Exception("Template upload failed: " + error));
                }
            }
        });
    }
    
    // ==================== OFFLINE SUPPORT ====================
    
    private void executeDispatch(String jobId, JSONObject payload, int retryCount) {
        executor.execute(() -> {
            try {
                Request request = new Request.Builder()
                    .url(baseUrl + "/ultra-dispatch")
                    .addHeader("Authorization", "Bearer " + apiKey)
                    .addHeader("Content-Type", "application/json")
                    .addHeader("X-SDK-Version", "2.1.0")
                    .post(RequestBody.create(
                        MediaType.parse("application/json"),
                        payload.toString()
                    ))
                    .build();
                
                try (Response response = httpClient.newCall(request).execute()) {
                    if (response.isSuccessful() && response.body() != null) {
                        String responseBody = response.body().string();
                        JSONObject json = new JSONObject(responseBody);
                        
                        // Remove from pending
                        removePendingJob(jobId);
                        
                        DispatchCallback callback = dispatchCallbacks.remove(jobId);
                        if (callback != null) {
                            mainHandler.post(() -> callback.onSuccess(json));
                        }
                        
                        Log.d(TAG, "Dispatch successful: " + jobId);
                    } else if (retryCount < MAX_RETRIES) {
                        long delay = RETRY_DELAY_MS * (long) Math.pow(2, retryCount);
                        mainHandler.postDelayed(() -> {
                            executeDispatch(jobId, payload, retryCount + 1);
                        }, delay);
                        
                        Log.w(TAG, "Retrying dispatch (" + (retryCount + 1) + "/" + MAX_RETRIES + ")");
                    } else {
                        DispatchCallback callback = dispatchCallbacks.remove(jobId);
                        if (callback != null) {
                            mainHandler.post(() -> callback.onError(
                                new Exception("Max retries reached: " + response.code())
                            ));
                        }
                    }
                }
            } catch (Exception e) {
                Log.e(TAG, "Dispatch failed", e);
                
                if (retryCount < MAX_RETRIES) {
                    long delay = RETRY_DELAY_MS * (long) Math.pow(2, retryCount);
                    mainHandler.postDelayed(() -> {
                        executeDispatch(jobId, payload, retryCount + 1);
                    }, delay);
                } else {
                    DispatchCallback callback = dispatchCallbacks.remove(jobId);
                    if (callback != null) {
                        mainHandler.post(() -> callback.onError(e));
                    }
                }
            }
        });
    }
    
    private void savePendingJob(String jobId, JSONObject payload) {
        if (prefs != null) {
            prefs.edit()
                .putString("pending_job_" + jobId, payload.toString())
                .apply();
        }
    }
    
    private void removePendingJob(String jobId) {
        if (prefs != null) {
            prefs.edit()
                .remove("pending_job_" + jobId)
                .apply();
        }
    }
    
    private void processPendingJobs() {
        if (prefs != null) {
            Map<String, ?> all = prefs.getAll();
            for (Map.Entry<String, ?> entry : all.entrySet()) {
                if (entry.getKey().startsWith("pending_job_")) {
                    String jobId = entry.getKey().substring(12);
                    String payloadStr = (String) entry.getValue();
                    
                    try {
                        JSONObject payload = new JSONObject(payloadStr);
                        executeDispatch(jobId, payload, 0);
                    } catch (Exception e) {
                        Log.e(TAG, "Error processing pending job", e);
                    }
                }
            }
        }
    }
    
    // ==================== INTERFACES ====================
    
    public interface DispatchCallback {
        void onSuccess(JSONObject response);
        void onError(Exception e);
    }
    
    public interface BatchCallback {
        void onSuccess(JSONObject response);
        void onError(Exception e);
    }
    
    public interface TemplateCallback {
        void onSuccess(String message, JSONObject response);
        void onError(String error);
    }
    
    public interface TemplateListCallback {
        void onSuccess(JSONArray templates);
        void onError(String error);
    }
    
    public interface SimpleCallback {
        void onSuccess();
        void onError(String error);
    }
    
    public static class DispatchRequest {
        public String userId;
        public String action;
        public JSONObject data;
        public Integer priority;
        
        public DispatchRequest(String userId, String action, JSONObject data) {
            this(userId, action, data, null);
        }
        
        public DispatchRequest(String userId, String action, JSONObject data, Integer priority) {
            this.userId = userId;
            this.action = action;
            this.data = data;
            this.priority = priority;
        }
    }
}