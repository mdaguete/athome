export class BlueskyAPI {
    constructor(baseURL = '') {
        this.baseURL = baseURL;
    }

    async getProfile(handle) {
        const response = await fetch(`${this.baseURL}/api/profile/${handle}`);
        if (!response.ok) {
            throw new Error('Failed to fetch profile');
        }
        return response.json();
    }

    async getFeed(handle, cursor) {
        const urlStr = this.baseURL ? `${this.baseURL}/api/feed/${handle}` : `/api/feed/${handle}`;
        const url = new URL(urlStr, window.location.origin);
        if (cursor) {
            url.searchParams.append('cursor', cursor);
        }
        const response = await fetch(url);
        if (!response.ok) {
            throw new Error('Failed to fetch feed');
        }
        return response.json();
    }

    async getPost(uri) {
        const response = await fetch(`${this.baseURL}/api/post/${uri}`);
        if (!response.ok) {
            throw new Error('Failed to fetch post');
        }
        return response.json();
    }

    async getPortfolioConfig() {
        const response = await fetch(`${this.baseURL}/api/portfolio-config`);
        if (!response.ok) {
            throw new Error('Failed to fetch portfolio configuration');
        }
        return response.json();
    }

    async getPortfolio(handle) {
        const response = await fetch(`${this.baseURL}/api/portfolio/${handle}`);
        if (!response.ok) {
            throw new Error('Failed to fetch portfolio');
        }
        return response.json();
    }
}
