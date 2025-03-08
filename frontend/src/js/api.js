export class BlueskyAPI {
    constructor() {
        // In development with Vite, we use relative URLs with /api prefix
        this.baseUrl = '/api';
    }

    async getProfile(handle) {
        try {
            const response = await fetch(`${this.baseUrl}/profile/${handle}`);
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            if (!data || !data.handle) {
                throw new Error('Invalid profile data received');
            }
            return data;
        } catch (error) {
            console.error('Error fetching profile:', error);
            throw error;
        }
    }

    async getFeed(handle, cursor = null) {
        try {
            const url = new URL(`${this.baseUrl}/feed/${handle}`, window.location.origin);
            if (cursor) {
                url.searchParams.append('cursor', cursor);
            }
            const response = await fetch(url);
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            console.log('Raw feed data:', JSON.stringify(data, null, 2));
            if (!data || !Array.isArray(data.feed)) {
                throw new Error('Invalid feed data received');
            }
            // Transform the feed data to match the expected structure
            const transformed = {
                cursor: data.cursor,
                posts: data.feed.map(item => {
                    if (!item || !item.post) return null;
                    const post = {
                        ...item.post,
                        // Ensure required fields exist
                        record: item.post.record || {},
                        author: item.post.author || {
                            handle: 'unknown',
                            displayName: 'Unknown',
                            avatar: ''
                        },
                        embed: item.post.embed || {},
                        replyCount: item.post.replyCount || 0,
                        repostCount: item.post.repostCount || 0,
                        likeCount: item.post.likeCount || 0
                    };
                    console.log('Transformed post:', JSON.stringify(post, null, 2));
                    return post;
                }).filter(post => post !== null) // Remove any invalid posts
            };
            console.log('Final transformed feed:', JSON.stringify(transformed, null, 2));
            return transformed;
        } catch (error) {
            console.error('Error fetching feed:', error);
            throw error;
        }
    }

    async getPost(uri) {
        console.log('Fetching post:', uri);
        try {
            const cleanUri = uri.replace(/^at:\/\//, '');
            const response = await fetch(`${this.baseUrl}/post/${cleanUri}`);
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            console.log('Raw post data:', JSON.stringify(data, null, 2));
            if (!data || !data.thread) {
                throw new Error('Invalid post data received');
            }

            // Extract post data from thread
            const threadPost = data.thread.post;
            if (!threadPost) {
                throw new Error('Post data not found in thread');
            }

            // Transform the post data
            const transformed = {
                ...threadPost,
                record: threadPost.record || {},
                author: threadPost.author || {
                    handle: 'unknown',
                    displayName: 'Unknown',
                    avatar: ''
                },
                embed: threadPost.embed || {},
                replyCount: threadPost.replyCount || 0,
                repostCount: threadPost.repostCount || 0,
                likeCount: threadPost.likeCount || 0,
                // Handle thread data
                parent: data.thread.parent ? data.thread.parent.post : null,
                replies: data.thread.replies ? data.thread.replies.map(reply => reply.post) : []
            };
            console.log('Transformed post:', JSON.stringify(transformed, null, 2));
            return transformed;
        } catch (error) {
            console.error('Error fetching post:', error);
            throw error;
        }
    }
}
