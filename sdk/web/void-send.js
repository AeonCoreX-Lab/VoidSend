class VoidSendSDK {
    constructor() {
        this.defaultBaseURL = 'https://api.voidsend.com/v2';
        this.baseURL = this.defaultBaseURL;
        this.apiKey = null;
        this.maxRetries = 3;
        this.pendingJobs = new Map();
        this.isInitialized = false;
        
        // Callback maps
        this.dispatchCallbacks = new Map();
        this.templateCallbacks = new Map();
        
        // Load from localStorage
        this.loadPendingJobs();
    }

    /**
     * Initialize SDK with custom base URL
     * @param {string} apiKey - Your API key
     * @param {string} baseURL - Custom base URL (optional)
     */
    initialize(apiKey, baseURL = null) {
        this.apiKey = apiKey;
        this.baseURL = baseURL || this.defaultBaseURL;
        this.isInitialized = true;
        console.log(`✅ VoidSend SDK initialized with base URL: ${this.baseURL}`);
        
        // Process pending jobs
        this.processPendingJobs();
    }

    // ==================== DISPATCH METHODS ====================

    async dispatch(userId, action, data = {}, options = {}) {
        if (!this.isInitialized) {
            throw new Error('SDK not initialized');
        }

        const jobId = this.generateUUID();
        const priority = options.priority || 5;
        const callback = options.callback;

        if (callback) {
            this.dispatchCallbacks.set(jobId, callback);
        }

        const payload = {
            user_id: userId,
            action: action,
            data: data,
            priority: priority,
            tags: {
                platform: 'web',
                ...options.tags
            }
        };

        // Save for offline
        this.savePendingJob(jobId, payload);

        // Execute
        return this.executeDispatch(jobId, payload, 0);
    }

    async dispatchBatch(jobs) {
        if (!this.isInitialized) {
            throw new Error('SDK not initialized');
        }

        try {
            const response = await fetch(`${this.baseURL}/batch-dispatch`, {
                method: 'POST',
                headers: this.getHeaders(),
                body: JSON.stringify({ jobs })
            });

            if (!response.ok) {
                throw new Error(`Server error: ${response.status}`);
            }

            return await response.json();
        } catch (error) {
            console.error('Batch dispatch failed:', error);
            throw error;
        }
    }

    // ==================== TEMPLATE MANAGEMENT ====================

    /**
     * Upload a custom HTML template
     * @param {string} action - Template action name
     * @param {string} htmlContent - HTML template content
     * @param {string} subject - Optional email subject
     * @param {Object} options - Callbacks
     * @returns {Promise}
     */
    async uploadTemplate(action, htmlContent, subject = null, options = {}) {
        if (!this.isInitialized) {
            throw new Error('SDK not initialized');
        }

        const requestId = this.generateUUID();
        const callback = options.callback;

        if (callback) {
            this.templateCallbacks.set(requestId, callback);
        }

        const payload = {
            action: action,
            html_content: htmlContent
        };
        if (subject) payload.subject = subject;

        try {
            const response = await fetch(`${this.baseURL}/template/upload`, {
                method: 'POST',
                headers: this.getHeaders(),
                body: JSON.stringify(payload)
            });

            const data = await response.json();
            const cb = this.templateCallbacks.get(requestId);
            this.templateCallbacks.delete(requestId);

            if (response.ok) {
                if (cb) cb.onSuccess('Template uploaded successfully', data);
                return data;
            } else {
                const error = new Error(data.error || 'Upload failed');
                if (cb) cb.onError(error.message);
                throw error;
            }
        } catch (error) {
            const cb = this.templateCallbacks.get(requestId);
            this.templateCallbacks.delete(requestId);
            if (cb) cb.onError(error.message);
            throw error;
        }
    }

    /**
     * List all templates
     */
    async listTemplates() {
        if (!this.isInitialized) {
            throw new Error('SDK not initialized');
        }

        const response = await fetch(`${this.baseURL}/template/list`, {
            method: 'GET',
            headers: this.getHeaders()
        });

        if (!response.ok) {
            throw new Error(`Failed to list templates: ${response.status}`);
        }

        const data = await response.json();
        return data.templates || [];
    }

    /**
     * Delete a template
     */
    async deleteTemplate(action) {
        if (!this.isInitialized) {
            throw new Error('SDK not initialized');
        }

        const response = await fetch(`${this.baseURL}/template/${action}`, {
            method: 'DELETE',
            headers: this.getHeaders()
        });

        if (!response.ok) {
            throw new Error(`Delete failed: ${response.status}`);
        }

        return true;
    }

    /**
     * Convenience: send email with custom HTML (auto-upload)
     */
    async dispatchWithHtml(userId, htmlContent, data = {}, callback = null) {
        const action = `custom_${Date.now()}`;
        
        await this.uploadTemplate(action, htmlContent, null, {
            callback: {
                onSuccess: (message, response) => {
                    this.dispatch(userId, action, data, { callback });
                },
                onError: (error) => {
                    if (callback) callback.onError(new Error(`Template upload failed: ${error}`));
                }
            }
        });
    }

    // ==================== INTERNAL METHODS ====================

    async executeDispatch(jobId, payload, retryCount, callback) {
        try {
            const response = await fetch(`${this.baseURL}/ultra-dispatch`, {
                method: 'POST',
                headers: this.getHeaders(),
                body: JSON.stringify(payload)
            });

            if (response.ok) {
                this.removePendingJob(jobId);
                const data = await response.json();
                const cb = this.dispatchCallbacks.get(jobId);
                this.dispatchCallbacks.delete(jobId);
                if (cb) cb.onSuccess(data);
                return data;
            } else if (retryCount < this.maxRetries) {
                const delay = Math.pow(2, retryCount) * 1000;
                await new Promise(resolve => setTimeout(resolve, delay));
                return this.executeDispatch(jobId, payload, retryCount + 1, callback);
            } else {
                const error = new Error(`Max retries reached: ${response.status}`);
                const cb = this.dispatchCallbacks.get(jobId);
                this.dispatchCallbacks.delete(jobId);
                if (cb) cb.onError(error);
                throw error;
            }
        } catch (error) {
            if (retryCount < this.maxRetries) {
                const delay = Math.pow(2, retryCount) * 1000;
                await new Promise(resolve => setTimeout(resolve, delay));
                return this.executeDispatch(jobId, payload, retryCount + 1, callback);
            } else {
                const cb = this.dispatchCallbacks.get(jobId);
                this.dispatchCallbacks.delete(jobId);
                if (cb) cb.onError(error);
                throw error;
            }
        }
    }

    getHeaders() {
        return {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${this.apiKey}`,
            'X-SDK-Version': '2.1.0',
            'X-SDK-Platform': 'web'
        };
    }

    savePendingJob(jobId, payload) {
        this.pendingJobs.set(jobId, payload);
        localStorage.setItem(`voidsend_${jobId}`, JSON.stringify(payload));
    }

    removePendingJob(jobId) {
        this.pendingJobs.delete(jobId);
        localStorage.removeItem(`voidsend_${jobId}`);
    }

    loadPendingJobs() {
        for (let i = 0; i < localStorage.length; i++) {
            const key = localStorage.key(i);
            if (key && key.startsWith('voidsend_')) {
                const jobId = key.substring(9);
                const payload = JSON.parse(localStorage.getItem(key));
                this.pendingJobs.set(jobId, payload);
            }
        }
    }

    processPendingJobs() {
        this.pendingJobs.forEach((payload, jobId) => {
            this.executeDispatch(jobId, payload, 0, null);
        });
    }

    generateUUID() {
        return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
            const r = Math.random() * 16 | 0;
            const v = c === 'x' ? r : (r & 0x3 | 0x8);
            return v.toString(16);
        });
    }
}

// ==================== FRAMEWORK INTEGRATIONS ====================

// React Hook
if (typeof React !== 'undefined') {
    VoidSendSDK.useVoidSend = function() {
        const [isReady, setIsReady] = React.useState(false);
        const [error, setError] = React.useState(null);

        React.useEffect(() => {
            if (window.voidSend) {
                setIsReady(true);
            }
        }, []);

        const dispatch = React.useCallback(async (userId, action, data) => {
            if (!window.voidSend) {
                throw new Error('VoidSend not initialized');
            }
            return window.voidSend.dispatch(userId, action, data);
        }, []);

        const uploadTemplate = React.useCallback(async (action, htmlContent, subject) => {
            if (!window.voidSend) {
                throw new Error('VoidSend not initialized');
            }
            return window.voidSend.uploadTemplate(action, htmlContent, subject);
        }, []);

        return { isReady, error, dispatch, uploadTemplate };
    };
}

// Vue Plugin
const VoidSendPlugin = {
    install(app, options) {
        const sdk = new VoidSendSDK();
        if (options?.apiKey) {
            sdk.initialize(options.apiKey, options.baseURL);
        }
        
        app.config.globalProperties.$voidsend = sdk;
        app.provide('voidsend', sdk);
    }
};

// Export for different environments
if (typeof module !== 'undefined' && module.exports) {
    module.exports = { VoidSendSDK, VoidSendPlugin };
} else {
    window.VoidSendSDK = VoidSendSDK;
    window.VoidSendPlugin = VoidSendPlugin;
}