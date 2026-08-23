/**
 * Custom Music Player Component for Besedka profiles.
 * Features: Play/Pause, track title/artist info, live seek bar, time display, volume control.
 * Supports resource cleanup when modal is closed.
 *
 * @param {Object} options
 * @param {string} options.songUrl - The URL of the audio file to play.
 * @param {string} [options.title] - Track title.
 * @param {string} [options.artist] - Artist name.
 * @returns {HTMLElement} The player container DOM element.
 */
export function createMusicPlayer({ songUrl, title, artist }) {
    const container = document.createElement('div');
    container.className = 'music-player-card';

    container.innerHTML = `
        <div class="music-player-header">
            <div class="music-player-disc">
                <span class="music-note-icon">🎵</span>
            </div>
            <div class="music-player-meta">
                <div class="music-track-title">${title ? escapeHTML(title) : 'Untitled Track'}</div>
                <div class="music-track-artist">${artist ? escapeHTML(artist) : 'Unknown Artist'}</div>
            </div>
        </div>
        <div class="music-player-controls">
            <button type="button" class="btn btn-secondary btn-icon music-play-btn" aria-label="Play">
                <span class="play-icon">▶</span>
            </button>
            <div class="music-time-container">
                <span class="music-time current-time">0:00</span>
                <input type="range" class="music-seeker" min="0" max="100" value="0" step="0.1" aria-label="Seek track position">
                <span class="music-time duration-time">0:00</span>
            </div>
            <button type="button" class="btn btn-secondary btn-icon music-volume-btn" aria-label="Mute/Unmute">
                <span class="volume-icon">🔊</span>
            </button>
        </div>
    `;

    const audio = new Audio(songUrl);
    audio.preload = 'auto';
    audio.className = 'music-player-audio-element';
    container.appendChild(audio);
    container.audio = audio;

    const playBtn = container.querySelector('.music-play-btn');
    const playIcon = container.querySelector('.play-icon');
    const seeker = container.querySelector('.music-seeker');
    const currentTimeEl = container.querySelector('.current-time');
    const durationTimeEl = container.querySelector('.duration-time');
    const volumeBtn = container.querySelector('.music-volume-btn');
    const volumeIcon = container.querySelector('.volume-icon');

    function formatTime(seconds) {
        if (Number.isNaN(seconds) || seconds === Infinity) return '0:00';
        const mins = Math.floor(seconds / 60);
        const secs = Math.floor(seconds % 60);
        return `${mins}:${secs < 10 ? '0' : ''}${secs}`;
    }

    function updateDuration() {
        if (audio.duration && !Number.isNaN(audio.duration) && audio.duration !== Infinity) {
            seeker.max = audio.duration;
            durationTimeEl.textContent = formatTime(audio.duration);
        }
    }

    let isSeeking = false;

    let isDestroyed = false;

    audio.addEventListener('loadedmetadata', updateDuration);
    audio.addEventListener('durationchange', updateDuration);
    audio.addEventListener('canplay', updateDuration);

    audio.addEventListener('timeupdate', () => {
        if (!isSeeking) {
            seeker.value = audio.currentTime || 0;
            currentTimeEl.textContent = formatTime(audio.currentTime);
        }
    });

    audio.addEventListener('ended', () => {
        playIcon.textContent = '▶';
        seeker.value = 0;
        currentTimeEl.textContent = '0:00';
    });

    audio.addEventListener('error', (e) => {
        if (isDestroyed || !audio.getAttribute('src')) return;
        console.error("Audio element error:", audio.error || e);
        playIcon.textContent = '▶';
        playBtn.title = "Audio playback failed";
    });

    playBtn.addEventListener('click', () => {
        if (audio.paused) {
            if (audio.error || audio.networkState === HTMLMediaElement.NETWORK_NO_SOURCE) {
                audio.load();
            }
            audio.play().then(() => {
                playIcon.textContent = '❚❚';
                playBtn.title = "Pause";
            }).catch(err => {
                if (isDestroyed) return;
                console.error("Audio playback error:", err);
                playIcon.textContent = '▶';
                playBtn.title = "Playback failed";
            });
        } else {
            audio.pause();
            playIcon.textContent = '▶';
            playBtn.title = "Play";
        }
    });

    seeker.addEventListener('input', () => {
        isSeeking = true;
        currentTimeEl.textContent = formatTime(seeker.value);
    });

    seeker.addEventListener('change', () => {
        const targetTime = parseFloat(seeker.value);
        if (!Number.isNaN(targetTime)) {
            audio.currentTime = targetTime;
        }
        isSeeking = false;
    });

    volumeBtn.addEventListener('click', () => {
        audio.muted = !audio.muted;
        volumeIcon.textContent = audio.muted ? '🔇' : '🔊';
    });

    // Cleanup method when modal/view is removed
    container.destroy = () => {
        isDestroyed = true;
        audio.pause();
        audio.removeAttribute('src');
        audio.load();
    };

    return container;
}

function escapeHTML(str) {
    if (!str) return '';
    return str.replace(/[&<>'"]/g, tag => ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        "'": '&#39;',
        '"': '&quot;'
    }[tag] || tag));
}
