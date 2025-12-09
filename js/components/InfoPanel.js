import { store } from '../state.js';

export function createInfoPanel(container) {
    const render = () => {
        container.innerHTML = `
            <div class="info-header">
                Info
            </div>
            <div class="info-content">
                <div class="info-section">
                    <div class="section-title">Last Document</div>
                    <div class="placeholder-box">
                        No documents yet
                    </div>
                </div>
                <div class="info-section">
                    <div class="section-title">Location</div>
                    <div class="placeholder-box">
                        Map View
                    </div>
                </div>
            </div>
        `;
    };

    render(store.state);
    store.subscribe(render);
}
