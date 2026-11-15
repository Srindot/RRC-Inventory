// Login state shared by every page: the session token and the user it
// belongs to (with their permissions), kept in localStorage.

const TOKEN_KEY = 'authToken';
const USER_KEY = 'authUser';

export function getToken() {
    try {
        return localStorage.getItem(TOKEN_KEY) || '';
    } catch {
        return '';
    }
}

export function getUser() {
    try {
        return JSON.parse(localStorage.getItem(USER_KEY) || 'null');
    } catch {
        return null;
    }
}

export function saveSession(token, user) {
    localStorage.setItem(TOKEN_KEY, token);
    localStorage.setItem(USER_KEY, JSON.stringify(user));
}

export function clearSession() {
    try {
        for (const key of [TOKEN_KEY, USER_KEY, 'adminToken', 'adminInfo', 'adminApiBase']) {
            localStorage.removeItem(key);
        }
    } catch {
        // storage blocked: nothing to clear
    }
}

export function can(user, perm) {
    return !!user?.permissions?.includes(perm);
}

// api is fetch with the session token attached. A 401 means the session has
// ended, so it is cleared.
export async function api(path, options = {}) {
    const token = getToken();
    const headers = { ...(options.headers || {}) };
    if (token) headers.Authorization = `Bearer ${token}`;
    const response = await fetch(path, { ...options, headers });
    if (response.status === 401 && token) clearSession();
    return response;
}

// refreshUser re-reads the user, since an admin may have changed their
// permissions. Returns null only when the server says the session is gone
// (401); any other failure (offline, 502 while the backend restarts) keeps
// what we have. Concurrent callers on one page share one request.
let inflight = null;
export function refreshUser() {
    if (!getToken()) return Promise.resolve(null);
    inflight ||= (async () => {
        let response;
        try {
            response = await api('/api/auth/me');
        } catch {
            return getUser();
        }
        if (response.status === 401) return null;
        if (!response.ok) return getUser();
        const user = await response.json();
        try {
            localStorage.setItem(USER_KEY, JSON.stringify(user));
        } catch {
            // storage blocked: still return the fresh user
        }
        return user;
    })().finally(() => (inflight = null));
    return inflight;
}

// loginUrl sends someone to log in and brings them back here afterwards,
// query included (e.g. /?borrow=<item>).
export function loginUrl() {
    return `/login?next=${encodeURIComponent(location.pathname + location.search)}`;
}
