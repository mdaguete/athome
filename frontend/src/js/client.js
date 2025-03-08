import { BlueskyAPI } from './api';

export class BlueskyClient {
    constructor() {
        this.api = new BlueskyAPI();
        this.feedContainer = document.getElementById('feed');
        this.profileContainer = document.getElementById('profile');
        this.postDetailContainer = document.getElementById('post-detail');
        this.loadingSpinner = document.getElementById('loading');
        this.currentHandle = null;
        this.cursor = null;
        this.isLoading = false;
        this.observer = null;

        // Set up infinite scroll
        this.setupInfiniteScroll();
        
        // Handle URL changes
        window.addEventListener('popstate', () => this.handleUrlChange());
        this.handleUrlChange();

        // Handle ESC key for closing detail view
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && this.postDetailContainer.style.display === 'flex') {
                this.closePostDetail();
            }
        });

        // Handle close button clicks via event delegation
        this.postDetailContainer.addEventListener('click', (e) => {
            if (e.target.classList.contains('close-button')) {
                this.closePostDetail();
            }
        });

        // Close post detail when clicking outside
        this.postDetailContainer.addEventListener('click', (e) => {
            if (e.target === this.postDetailContainer) {
                this.closePostDetail();
            }
        });
    }

    setupInfiniteScroll() {
        // Create intersection observer for infinite scroll
        this.observer = new IntersectionObserver(
            (entries) => {
                // Only load more if we're intersecting, not already loading, and have a cursor
                if (entries[0].isIntersecting && !this.isLoading && this.cursor) {
                    this.loadMorePosts();
                }
            },
            { rootMargin: '200px' }
        );
    }

    async handleUrlChange() {
        try {
            // Get handle and post URI from URL
            const url = new URL(window.location.href);
            const params = new URLSearchParams(url.search);
            let handle = params.get('handle');
            const postUri = params.get('post');
            
            if (!handle) {
                // Get default handle from HTML data attribute
                handle = document.documentElement.getAttribute('data-default-handle');
                // Update URL without reloading
                const newUrl = new URL(window.location.href);
                newUrl.searchParams.set('handle', handle);
                window.history.pushState({}, '', newUrl);
            }
            
            if (handle !== this.currentHandle) {
                // Reset state for new handle
                this.currentHandle = handle;
                this.cursor = null;
                this.feedContainer.innerHTML = '';
                
                // Disconnect any existing observers
                if (this.observer) {
                    this.observer.disconnect();
                }
                
                await this.loadProfile();
                await this.loadMorePosts();
            }

            // If there's a post URI in the URL, show its detail view
            if (postUri) {
                await this.showPostDetail(postUri);
            }
        } catch (error) {
            console.error('Error handling URL change:', error);
            this.showError('Failed to process the URL. Please check the handle parameter.');
        }
    }

    showError(message) {
        const errorElement = document.createElement('div');
        errorElement.className = 'error-message';
        errorElement.textContent = message;
        
        // Add error to feed container
        if (this.feedContainer) {
            this.feedContainer.insertBefore(errorElement, this.feedContainer.firstChild);
        }
        
        // Auto-hide after 5 seconds
        setTimeout(() => {
            errorElement.remove();
        }, 5000);
    }

    async loadProfile() {
        try {
            const profile = await this.api.getProfile(this.currentHandle);
            if (!profile) {
                throw new Error('Profile not found');
            }

            this.profileContainer.innerHTML = '';

            // Create banner container
            const bannerContainer = document.createElement('div');
            bannerContainer.className = 'profile-banner-container';
            if (profile.banner) {
                const banner = document.createElement('img');
                banner.src = this.sanitizeUrl(profile.banner);
                banner.alt = 'Profile banner';
                banner.className = 'profile-banner';
                banner.onerror = () => {
                    banner.style.display = 'none';
                };
                bannerContainer.appendChild(banner);
            }
            this.profileContainer.appendChild(bannerContainer);

            // Create avatar container that overlaps banner
            const avatarContainer = document.createElement('div');
            avatarContainer.className = 'profile-avatar-container';
            const avatar = document.createElement('img');
            avatar.src = this.sanitizeUrl(profile.avatar || '');
            avatar.alt = 'Profile avatar';
            avatar.className = 'profile-avatar';
            avatar.onerror = () => {
                avatar.src = 'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"%3E%3Cpath fill="%23ccc" d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 3c1.66 0 3 1.34 3 3s-1.34 3-3 3-3-1.34-3-3 1.34-3 3-3zm0 14.2c-2.5 0-4.71-1.28-6-3.22.03-1.99 4-3.08 6-3.08 1.99 0 5.97 1.09 6 3.08-1.29 1.94-3.5 3.22-6 3.22z"%3E%3C/path%3E%3C/svg%3E';
            };
            avatarContainer.appendChild(avatar);
            this.profileContainer.appendChild(avatarContainer);

            // Create profile content
            const content = document.createElement('div');
            content.className = 'profile-content';

            const displayName = document.createElement('h1');
            displayName.textContent = this.sanitizeText(profile.displayName || 'Unknown');
            displayName.className = 'profile-name';

            const handle = document.createElement('div');
            // Add handle with @ symbol and as a link to bluesky profile
            handle.innerHTML = `<a href="https://bsky.app/profile/${this.sanitizeText(profile.handle || 'unknown')}">@${this.sanitizeText(profile.handle || 'unknown')}</a>`;
            handle.className = 'profile-handle';


            const stats = document.createElement('div');
            stats.className = 'profile-stats';

            content.appendChild(displayName);
            content.appendChild(handle);


            this.profileContainer.appendChild(content);
        } catch (error) {
            console.error('Error loading profile:', error);
            this.showError('Failed to load profile. Please try again later.');
            
            // Clear profile container and show minimal error state
            this.profileContainer.innerHTML = '';
            const errorHeader = document.createElement('h1');
            errorHeader.textContent = 'Profile Not Available';
            errorHeader.className = 'profile-name error';
            this.profileContainer.appendChild(errorHeader);
        }
    }

    async loadMorePosts() {
        if (this.isLoading) return;
        
        this.isLoading = true;
        this.loadingSpinner.style.display = 'block';

        try {
            const data = await this.api.getFeed(this.currentHandle, this.cursor);
            
            // If we get posts, update cursor and add them
            if (data.posts && data.posts.length > 0) {
                this.cursor = data.cursor;
                data.posts.forEach(post => {
                    const postElement = this.createPostElement(post);
                    this.feedContainer.appendChild(postElement);
                    
                    // Only observe the last post if we have a cursor for more posts
                    if (post === data.posts[data.posts.length - 1] && this.cursor) {
                        this.observer.observe(postElement);
                    }
                });
            } else {
                // No more posts to load
                this.cursor = null;
            }
        } catch (error) {
            console.error('Error loading posts:', error);
            this.showError('Failed to load posts. Please try again later.');
        } finally {
            this.isLoading = false;
            this.loadingSpinner.style.display = 'none';
        }
    }

    createPostElement(post) {
        const postElement = document.createElement('article');
        postElement.className = 'post';


        // Add date if available
        let dateInfo = null;
        if (post.indexedAt) {
            dateInfo = document.createElement('div');
            dateInfo.className = 'post-date-botton';
            const date = new Date(post.indexedAt);
            dateInfo.title = date.toLocaleString(); // Full date on hover
            dateInfo.textContent = this.formatDate(date);
        }



        const content = document.createElement('div');
        content.className = 'post-content';

        // Images are in embed.images
        if (post.embed && post.embed.images && post.embed.images.length > 0) {
            const imageGrid = document.createElement('div');
            imageGrid.className = 'post-image-grid';

            post.embed.images.forEach(image => {
                const imageContainer = document.createElement('div');
                imageContainer.className = 'post-image-container';

                const img = document.createElement('img');
                img.src = this.sanitizeUrl(image.thumb);
                img.alt = image.alt || 'Post image';
                img.className = 'post-image';
                img.style.objectFit = 'contain';
                if (image.aspectRatio) {
                    imageContainer.style.aspectRatio = image.aspectRatio;
                }
                img.onerror = () => {
                    imageContainer.style.display = 'none';
                };

                imageContainer.appendChild(img);
                imageGrid.appendChild(imageContainer);
            });

            content.appendChild(imageGrid);
        }

        // Text is in record.text
        if (post.record && post.record.text) {
            const text = document.createElement('div');
            text.className = 'post-text';
            text.textContent = this.sanitizeText(post.record.text);
            content.appendChild(text);
        }

        postElement.appendChild(content);
        postElement.appendChild(dateInfo);

        // Add click handler for post detail view
        postElement.addEventListener('click', () => {
            if (post.uri) {
                this.showPostDetail(post.uri);
            }
        });

        return postElement;
    }

    createThreadPost(post) {
        const threadPost = document.createElement('article');
        threadPost.className = 'thread-post';

        const header = document.createElement('header');
        header.className = 'post-detail-header';

        // Check if author data exists
        if (post.author) {
            const avatar = document.createElement('img');
            avatar.src = this.sanitizeUrl(post.author.avatar || '');
            avatar.alt = 'Author avatar';
            avatar.className = 'post-avatar';
            avatar.onerror = () => {
                avatar.src = 'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"%3E%3Cpath fill="%23ccc" d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 3c1.66 0 3 1.34 3 3s-1.34 3-3 3-3-1.34-3-3 1.34-3 3-3zm0 14.2c-2.5 0-4.71-1.28-6-3.22.03-1.99 4-3.08 6-3.08 1.99 0 5.97 1.09 6 3.08-1.29 1.94-3.5 3.22-6 3.22z"%3E%3C/path%3E%3C/svg%3E';
            };

            const authorInfo = document.createElement('div');
            authorInfo.className = 'post-author-info';

            const authorName = document.createElement('div');
            authorName.className = 'post-author-name';
            authorName.textContent = this.sanitizeText(post.author.displayName || 'Unknown');

            const authorHandle = document.createElement('div');
            authorHandle.className = 'post-author-handle';
            authorHandle.innerHTML = `<a href="https://bsky.app/profile/${this.sanitizeText(post.author.handle || 'unknown')}">@${this.sanitizeText(post.author.handle || 'unknown')}</a>`;

            authorInfo.appendChild(authorName);
            authorInfo.appendChild(authorHandle);

            header.appendChild(avatar);
            header.appendChild(authorInfo);

            // Add date if available
            if (post.indexedAt) {
                const dateInfo = document.createElement('div');
                dateInfo.className = 'post-date';
                const date = new Date(post.indexedAt);
                dateInfo.title = date.toLocaleString(); // Full date on hover
                dateInfo.textContent = this.formatDate(date);
                header.appendChild(dateInfo);
            }
        }

        const content = document.createElement('div');
        content.className = 'post-detail-content';

        // Text is in record.text
        if (post.record && post.record.text) {
            const text = document.createElement('div');
            text.className = 'post-detail-text';
            text.textContent = this.sanitizeText(post.record.text);
            content.appendChild(text);
        }

        // Images are in embed.images
        if (post.embed && post.embed.images && post.embed.images.length > 0) {
            const imageGrid = document.createElement('div');
            imageGrid.className = 'post-detail-image-grid';

            post.embed.images.forEach(image => {
                const imageContainer = document.createElement('div');
                imageContainer.className = 'post-detail-image-container';

                const img = document.createElement('img');
                img.src = this.sanitizeUrl(image.fullsize);
                img.alt = image.alt || 'Post image';
                img.className = 'post-image';
                img.style.objectFit = 'contain';
                if (image.aspectRatio) {
                    imageContainer.style.aspectRatio = image.aspectRatio;
                }
                img.onerror = () => {
                    imageContainer.style.display = 'none';
                };

                imageContainer.appendChild(img);
                imageGrid.appendChild(imageContainer);
            });

            content.appendChild(imageGrid);
        }

        threadPost.appendChild(header);
        threadPost.appendChild(content);

        return threadPost;
    }

    async showPostDetail(uri) {
        console.log('Showing post detail:', uri);
        try {
            const post = await this.api.getPost(uri);
            if (!post) {
                throw new Error('Post not found');
            }
            console.log('Post data on detail:', post);
            const detailContent = document.createElement('div');
            detailContent.className = 'post-detail';
            
            // Add close button
            const closeButton = document.createElement('button');
            closeButton.className = 'close-button';
            closeButton.innerHTML = '×';
            closeButton.onclick = () => this.closePostDetail();
            detailContent.appendChild(closeButton);

            // Add parent thread if exists and is valid
            if (post.parent && typeof post.parent === 'object') {
                const parentThread = document.createElement('div');
                parentThread.className = 'parent-thread';
                parentThread.appendChild(this.createThreadPost(post.parent));
                detailContent.appendChild(parentThread);
            }

            // Add main post
            const mainPost = document.createElement('div');
            mainPost.className = 'main-post';
            mainPost.appendChild(this.createThreadPost(post));
            detailContent.appendChild(mainPost);

            // Add replies if exist and are valid
            if (post.replies && Array.isArray(post.replies) && post.replies.length > 0) {
                const repliesContainer = document.createElement('div');
                repliesContainer.className = 'replies-container';
                post.replies.forEach(reply => {
                    if (reply && typeof reply === 'object') {
                        const replyThread = document.createElement('div');
                        replyThread.className = 'reply-thread';
                        replyThread.appendChild(this.createThreadPost(reply));
                        repliesContainer.appendChild(replyThread);
                    }
                });
                detailContent.appendChild(repliesContainer);
            }

            this.postDetailContainer.innerHTML = '';
            this.postDetailContainer.appendChild(detailContent);
            this.postDetailContainer.style.display = 'flex';

            // Update URL without reload
            const newUrl = new URL(window.location.href);
            newUrl.searchParams.set('post', uri);
            window.history.pushState({}, '', newUrl);
        } catch (error) {
            console.error('Error showing post detail:', error);
            this.showError('Failed to load post detail. Please try again later.');
            this.closePostDetail();
        }
    }

    // Helper function to format dates in a user-friendly way
    formatDate(date) {
        const now = new Date();
        const diff = now - date;
        const seconds = Math.floor(diff / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);

        if (seconds < 60) {
            return 'just now';
        } else if (minutes < 60) {
            return `${minutes}m`;
        } else if (hours < 24) {
            return `${hours}h`;
        } else if (days < 7) {
            return `${days}d`;
        } else {
            return date.toLocaleDateString();
        }
    }

    closePostDetail() {
        this.postDetailContainer.style.display = 'none';
        // Remove post from URL without reload
        const newUrl = new URL(window.location.href);
        newUrl.searchParams.delete('post');
        window.history.pushState({}, '', newUrl);
    }

    sanitizeUrl(url) {
        try {
            const parsed = new URL(url);
            return parsed.href;
        } catch (e) {
            console.error('Invalid URL:', url);
            return '';
        }
    }

    sanitizeText(text) {
        if (!text) return '';
        const div = document.createElement('div');
        div.textContent = text;
        return div.textContent;
    }

    createStat(value, label) {
        const stat = document.createElement('div');
        stat.className = 'post-stat';
        stat.textContent = `${value || 0} ${label}`;
        return stat;
    }
}
