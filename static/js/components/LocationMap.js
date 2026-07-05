let d3LoadPromise = null;

export class LocationMap {
    constructor(container, store) {
        this.container = container;
        this.store = store;
        
        this.width = container.clientWidth || 300;
        this.height = container.clientHeight || 200;
        
        this.init();
    }

    async init() {
        try {
            await this.loadD3();
            
            this.svg = d3.select(this.container).append("svg")
                .attr("width", "100%")
                .attr("height", "100%")
                .attr("viewBox", `0 0 ${this.width} ${this.height}`)
                .attr("preserveAspectRatio", "xMidYMid meet");

            // SVG Background
            this.svg.append("rect")
                .attr("width", "100%")
                .attr("height", "100%")
                .attr("fill", "#0f172a");
                
            this.projection = d3.geoMercator()
                .scale(this.height / 6.2)
                .translate([this.width / 2, this.height / 2]);

            this.path = d3.geoPath().projection(this.projection);

            this.mapLayer = this.svg.append("g").attr("class", "map-layer");
            this.markerLayer = this.svg.append("g").attr("class", "marker-layer");

            // Sphere background
            this.mapLayer.append("path")
                .datum({type: "Sphere"})
                .attr("class", "sphere")
                .attr("d", this.path)
                .attr("fill", "#0f172a") // ocean
                .attr("stroke", "rgba(255, 255, 255, 0.1)");
                
            this.worldData = null;

            // Resize observer
            this.resizeObserver = new ResizeObserver(entries => {
                for (let entry of entries) {
                    this.width = entry.contentRect.width || 300;
                    this.height = entry.contentRect.height || 200;
                    this.svg.attr("viewBox", `0 0 ${this.width} ${this.height}`);
                    this.updateProjection();
                }
            });
            this.resizeObserver.observe(this.container);

            await this.loadMapData();

            this.store.subscribe((state) => {
                this.updateMarkers(state);
            });
            
            // Initial render
            this.updateMarkers(this.store.state);
        } catch (error) {
            console.error("Map initialization failed:", error);
        }
    }

    loadD3() {
        if (window.d3) return Promise.resolve();
        if (d3LoadPromise) return d3LoadPromise;

        d3LoadPromise = new Promise((resolve, reject) => {
            if (window.d3) {
                resolve();
                return;
            }

            // Remove any existing failed/stuck script tag
            const existingScript = document.querySelector('script[src="/js/d3.min.js"]');
            if (existingScript) {
                existingScript.remove();
            }

            const script = document.createElement('script');
            script.src = '/js/d3.min.js';
            script.onload = () => resolve();
            script.onerror = () => {
                d3LoadPromise = null;
                script.remove();
                reject(new Error('D3 load failed'));
            };
            document.head.appendChild(script);
        });

        return d3LoadPromise;
    }

    async loadMapData() {
        try {
            const response = await fetch('/world.geojson');
            const data = await response.json();
            this.worldData = data;
            
            this.mapLayer.selectAll(".land")
                .data(data.features)
                .enter().append("path")
                .attr("class", "land")
                .attr("d", this.path)
                .attr("fill", "#334155")
                .attr("stroke", "#1e293b")
                .attr("stroke-width", 0.5);
                
            this.updateProjection();
        } catch (error) {
            console.error("Failed to load map data:", error);
        }
    }
    
    // Convert lat/lng to 3D Cartesian coords on a unit sphere
    toFixed3D(lon, lat) {
        const rad = Math.PI / 180;
        const lambda = lon * rad;
        const phi = lat * rad;
        return [
            Math.cos(phi) * Math.cos(lambda),
            Math.cos(phi) * Math.sin(lambda),
            Math.sin(phi)
        ];
    }
    
    // Convert 3D Cartesian coords back to lat/lng
    fromFixed3D(x, y, z) {
        const rad = Math.PI / 180;
        return [
            Math.atan2(y, x) / rad,
            Math.asin(z) / rad
        ];
    }

    updateProjection() {
        const minScale = this.width / (2 * Math.PI);
        // The vendored world.geojson is low-resolution (Natural Earth 110m), so
        // zooming in tighter than this makes the coarse coastlines look blocky.
        // Cap the fit-to-members zoom at the same close-up level used for a
        // single user, so nearby members never zoom past the map's resolution.
        const maxScale = Math.max(minScale, Math.min(this.width, this.height) * 1.5);

        if (!this.usersLastLocations || this.usersLastLocations.length === 0) {
            this.projection
                .scale(minScale)
                .translate([this.width / 2, this.height / 2])
                .center([0, 0]);
                
            this.drawPaths();
            return;
        }

        const points = {
            type: "MultiPoint",
            coordinates: this.usersLastLocations.map(l => [l.lng, l.lat])
        };

        // If only one user, center on them and zoom in
        if (this.usersLastLocations.length === 1) {
             this.projection
                .scale(maxScale)
                .translate([this.width / 2, this.height / 2])
                .center([this.usersLastLocations[0].lng, this.usersLastLocations[0].lat]);
        } else {
             // Reset center before fitting
             this.projection.center([0, 0]);

             // We fit the projection using fitExtent with some padding
             this.projection.fitExtent([
                 [this.width * 0.15, this.height * 0.15],
                 [this.width * 0.85, this.height * 0.85]
             ], points);

             // Enforce that scale is at least minScale to fill container width
             let currentScale = this.projection.scale();
             if (currentScale < minScale) {
                 this.projection
                     .scale(minScale)
                     .translate([this.width / 2, this.height / 2])
                     .center([0, 0]);
             }
             // Cap the zoom so close-together members don't zoom past the
             // vendored map's resolution. Reducing scale keeps the fitted
             // bounding-box centre (already at the container centre) in place.
             currentScale = this.projection.scale();
             if (currentScale > maxScale) {
                 this.projection.scale(maxScale);
             }
        }

        this.drawPaths();
    }

    drawPaths() {
        this.path.projection(this.projection);
        this.mapLayer.select('.sphere').attr('d', this.path);
        this.mapLayer.selectAll('.land').attr('d', this.path);
        
        // Update markers position
        this.markerLayer.selectAll('.marker-container')
            .attr('transform', d => {
                const projected = this.projection([d.location.lng, d.location.lat]);
                if (projected) {
                    return `translate(${projected[0]}, ${projected[1]})`;
                }
                return 'translate(-9999, -9999)';
            });
            
        // Show all markers on flat map
        this.markerLayer.selectAll('.marker-container')
            .style('display', null);
    }

    getDMID(u1, u2) {
        const ids = [u1, u2];
        ids.sort();
        return `dm_${ids[0]}_${ids[1]}`;
    }

    updateMarkers(state) {
        if (!state.userLocations) return;

        // Convert Map to array and filter out expired (TTL 10 mins)
        const now = Date.now();
        const ttl = 10 * 60 * 1000;
        
        const validLocations = Array.from(state.userLocations.entries())
            .filter(([_userId, loc]) => {
                // Must not be expired
                return (now - loc.timestamp) <= ttl; 
            })
            .map(([userId, loc]) => {
                let user = state.users.find(u => u.id === userId);
                if (!user && state.currentUser?.id === userId) {
                    user = state.currentUser;
                }
                return {
                    userId,
                    location: loc,
                    user: user || { id: userId, displayName: 'Unknown' }
                };
            });
            
        // Check if locations actually changed (to avoid unnecessary projection reculculations)
        const oldIds = (this.usersLastLocations || []).map(u => u.userId).sort().join(',');
        const newIds = validLocations.map(u => u.userId).sort().join(',');
        const locsChanged = oldIds !== newIds || !this.usersLastLocations || validLocations.some(vl => {
            const old = this.usersLastLocations.find(ol => ol.userId === vl.userId);
            return !old || old.lat !== vl.location.lat || old.lng !== vl.location.lng;
        });

        this.usersLastLocations = validLocations.map(v => ({userId: v.userId, lat: v.location.lat, lng: v.location.lng}));

        if (locsChanged) {
            this.updateProjection();
        }

        // Data join
        const markers = this.markerLayer.selectAll('.marker-container')
            .data(validLocations, d => d.userId);

        markers.exit().remove();

        // Enter
        const enter = markers.enter().append('g')
            .attr('class', 'marker-container')
            .style('cursor', 'pointer')
            .on('click', (_event, d) => {
                if (d.userId !== this.store.state.currentUser?.id) {
                    const dmId = this.getDMID(this.store.state.currentUser.id, d.userId);
                    this.store.setActiveChat(dmId);
                }
            });

        // Add Avatar Background
        enter.append('circle')
            .attr('r', 12)
            .attr('fill', '#3b82f6')
            .attr('stroke', '#ffffff')
            .attr('stroke-width', 2);

        // Add Avatar Image or Initials
        enter.each(function(d) {
            const el = d3.select(this);
            const user = d.user;
            const name = user.displayName || user.userName || user.name || user.id;
            
            if (user.avatarUrl) {
                // For images we need the standard svg image or foreignObject
                el.append('image')
                    .attr('href', user.avatarUrl)
                    .attr('x', -12)
                    .attr('y', -12)
                    .attr('width', 24)
                    .attr('height', 24)
                    .attr('clip-path', 'circle(12px)');
                
                // create clip path if needed or just use foreignObject which handles css border-radius better
                const fo = el.append("foreignObject")
                    .attr("x", -12)
                    .attr("y", -12)
                    .attr("width", 24)
                    .attr("height", 24);
                
                fo.append("xhtml:img")
                    .attr("src", user.avatarUrl)
                    .attr("title", name)
                    .style("width", "100%")
                    .style("height", "100%")
                    .style("border-radius", "50%")
                    .style("object-fit", "cover")
                    .style("pointer-events", "none")
                    .style("border", "2px solid white")
                    .style("box-sizing", "border-box");
                    
                // Optional: remove standard circle, we just use foreign object
                el.select('circle').remove();
                el.select('image').remove();
            } else {
                // Initials
                const initial = name.charAt(0).toUpperCase();
                el.append('text')
                    .attr('text-anchor', 'middle')
                    .attr('dominant-baseline', 'central')
                    .attr('font-size', '10px')
                    .attr('font-weight', 'bold')
                    .attr('fill', '#ffffff')
                    .text(initial)
                    .append('title').text(name); // Tooltip
            }
        });
        
        // Add title for native tooltips on hover for all
        enter.append('title').text(d => d.user.displayName || d.user.userName || d.user.name || d.userId);

        this.drawPaths();
    }
}
