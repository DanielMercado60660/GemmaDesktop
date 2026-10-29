exports.solve=function(x){const out={};for(const e of x.events){if(e.at>=x.now-x.window&&e.at<=x.now)out[e.user]=(out[e.user]||0)+e.units;}return out;};
