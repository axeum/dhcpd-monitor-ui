new Vue({
    el: '#app',

    data: {
        ws: null, // Our websocket
        allState: '' // A running list of states displayed on the screen
    },

    created: function() {
        var self = this;
        this.ws = new WebSocket('ws://' + window.location.host + '/ws');
        this.ws.addEventListener('message', function(e) {
            self.allState = JSON.parse(e.data)
        });
    }
});
