/*
  ztncui - ZeroTier network controller UI
  Copyright (C) 2017-2021  Key Networks (https://key-networks.com)
  Licensed under GPLv3 - see LICENSE for details.
*/

require('dotenv').config();

const express = require('express');
const path = require('path');
const favicon = require('serve-favicon');
const logger = require('morgan');
const cookieParser = require('cookie-parser');
const bodyParser = require('body-parser');
const expressValidator = require('express-validator');
const session = require('express-session');
const helmet = require('helmet');

const index = require('./routes/index');
const users = require('./routes/users');
const zt_controller = require('./routes/zt_controller');

const app = express();

// Base path support for reverse proxy subpaths (e.g., Nginx location)
// Example: BASE_PATH=/ztncui -> app is served under http(s)://host/ztncui
const rawBasePath = process.env.BASE_PATH || '';
const basePath = (function normalizeBasePath(p) {
  if (!p) return '';
  if (!p.startsWith('/')) p = '/' + p;
  // trim trailing slash except root
  if (p.length > 1 && p.endsWith('/')) p = p.slice(0, -1);
  return p;
})(rawBasePath);
app.locals.basePath = basePath;

const session_secret = Math.random().toString(36).substring(2,12);

// view engine setup
app.set('views', path.join(__dirname, 'views'));
app.set('view engine', 'pug');

app.use(helmet());
// Mount favicon and static assets under base path
app.use(basePath, favicon(path.join(__dirname, 'public', 'favicon.ico')));
app.use(logger('dev'));
app.use(bodyParser.json());
app.use(bodyParser.urlencoded({ extended: false }));
app.use(session({
  resave: false,
  saveUninitialized: false,
  secret: session_secret
}));
app.use(expressValidator());
app.use(cookieParser());
// Inject basePath into templates
app.use(function(req, res, next) {
  res.locals.basePath = basePath;
  next();
});

// Static mounts under base path
app.use(basePath, express.static(path.join(__dirname, 'public')));
app.use(basePath + '/fonts', express.static(path.join(__dirname, 'node_modules/bootstrap/fonts')));
app.use(basePath + '/bscss', express.static(path.join(__dirname, 'node_modules/bootstrap/dist/css')));
app.use(basePath + '/jqjs', express.static(path.join(__dirname, 'node_modules/jquery/dist')));
app.use(basePath + '/bsjs', express.static(path.join(__dirname, 'node_modules/bootstrap/dist/js')));

// Route mounts under base path
app.use(basePath + '/', index);
app.use(basePath + '/users', users);
app.use(basePath + '/controller', zt_controller);

// catch 404 and forward to error handler
app.use(function(req, res, next) {
  var err = req.session.error;
  var msg = req.session.success;
  delete req.session.error;
  delete req.session.success;
  res.locals.message = '';
  if (err) res.locals.message = '<p class="msg error">' + err + '</p>';
  if (msg) res.locals.message = '<p class="msg success">' + msg + '</p>';
next();
});

// error handler
app.use(function(err, req, res, next) {
  // set locals, only providing error in development
  res.locals.message = err.message;
  res.locals.error = req.app.get('env') === 'development' ? err : {};

  // render the error page
  res.status(err.status || 500);
  res.render('error');
});

module.exports = app;
