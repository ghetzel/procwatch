"use strict";

$(function(){
    var guid = function(sep) {
        if(sep === undefined){
            sep = '-';
        }

        function s4() {
            return Math.floor((1 + Math.random()) * 0x10000).toString(16).substring(1);
        }

        return s4() + s4() + sep + s4() + sep + s4() + sep + s4() + sep + s4() + s4() + s4();
    };

    var Procwatch = Stapes.subclass({
        constructor: function(){
            // prevent normal form submissions, we'll handle them here
            $('form').on('submit', function(e){
                this.submitForm(e);
                e.preventDefault();
            }.bind(this));

            this.setupPartials();
        },

        setupPartials: function(){
            this._partials = {};

            $('[partial-load]').each(function(i, element){
                element = $(element);
                var id = element.attr('id');

                if(!id){
                    id = 'partial_' + guid('');
                    element.attr('id', id);
                }

                // setup partial from element
                if(!this._partials[id]){
                    var partial = new Partial(
                        id,
                        element,
                        element.attr('partial-load'), {
                            'interval': element.attr('partial-interval'),
                            'onload': element.attr('partial-onload'),
                        });

                    // load the partial and, if an interval is given, start a timer to
                    // periodically reload
                    partial.init();

                    this._partials[id] = partial;
                }

            }.bind(this));
        },

        notify: function(message, type, details, config){
            $.notify($.extend(details, {
                'message': message,
            }), $.extend(config, {
                'type': (type || 'info'),
            }));
        },

        actionProgram: function(name, action){
            $.ajax('/api/programs/'+name+'/action/'+action, {
                method: 'PUT',
            })
        },

        submitForm: function(event){
            var form = $(event.target);
            var url = '';

            if(form.action && form.action.length > 0){
                url = form.action;
            }else if(name = form.attr('name')){
                url = '/api/' + name;
            }else{
                this.notify('Could not determine path to submit data to', 'error');
                return;
            }

            var createNew = true;
            var record = {
                'fields': {},
            };

            $.each(form.serializeArray(), function(i, field) {
                if(field.value == '' || field.value == '0'){
                    delete field['value'];
                }

                if(field.name == "id"){
                    if(field.value){
                        createNew = false;
                    }

                    record['id'] = field.value;
                }else if(field.value !== undefined){
                    record['fields'][field.name] = field.value;
                }
            });

            $.ajax(url, {
                method: (form.attr('method') || (createNew ? 'POST' : 'PUT')),
                data: JSON.stringify({
                    'records': [record],
                }),
                success: function(){
                    var redirectTo = (form.data('redirect-to') || '/'+form.attr('name'));
                    location.href = redirectTo;
                }.bind(this),
                error: this.showResponseError.bind(this),
            })
        },

        showResponseError: function(response){
            this.notify(response.responseText, 'danger', {
                'icon': 'fa fa-warning',
                'title': '<b>' +
                    response.statusText + ' (HTTP '+response.status.toString()+')' +
                    '<br />' +
                '</b>',
            });
        },
    });

    var Partial = Stapes.subclass({
        constructor: function(id, element, url, options){
            this.id = id;
            this.element = element;
            this.url = url;
            this.options = (options || {});
        },

        init: function(){
            // console.debug('Initializing partial', '#'+this.id, this.options);

            this.load();

            // this is a no-op if autoreloading isn't requested
            this.monitor();
        },

        clear: function(){
            this.element.empty();
        },

        load: function(){
            if(this.url) {
                $.ajax({
                    url: this.url,
                    timeout: 1000,
                    success: function(response){
                        $(this.element).html(response);

                        if(this.options.onload){
                            eval(this.options.onload);
                        }
                    }.bind(this),
                    error: function(response){
                        if(this.options.onerror){
                            eval(this.options.onerror);
                        }else{
                            this.clear();
                        }
                    }.bind(this),
                });
            }
        },

        monitor: function(){
            // setup the interval if it exists and is <= 60 updates/sec.
            if(this.options.interval > 8 && !this._interval){
                this._interval = window.setInterval(this.load.bind(this), this.options.interval);
            }
        },

        stop: function(){
            if(this._interval){
                window.clearInterval(this._interval);
            }
        },
    });

    window.procwatch = new Procwatch();
});
