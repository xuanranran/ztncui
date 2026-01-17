/*
  ztncui - ZeroTier network controller UI
  Copyright (C) 2017-2021  Key Networks (https://key-networks.com)
  Licensed under GPLv3 - see LICENSE for details.
*/

const express = require('express');
const auth = require('../controllers/auth');
const authenticate = auth.authenticate;
const restrict = auth.restrict;
const router = express.Router();

/** Redirect logged user to controler page */
function guest_only(req, res, next) {
  if (req.session.user) {
    const basePath = req.app && req.app.locals ? (req.app.locals.basePath || '') : '';
    res.redirect(basePath + '/controller');
  } else {
    next();
  }
}

/* GET home page. */
router.get('/', guest_only, function (req, res, next) {
  res.render('front_door', { title: 'ztncui控制器' });
});

router.get('/logout', function (req, res) {
  req.session.destroy(function () {
    const basePath = req.app && req.app.locals ? (req.app.locals.basePath || '') : '';
    res.redirect(basePath + '/');
  });
});

router.get('/login', guest_only, function (req, res) {
  let message = null;
  if (req.session.error) {
    if (req.session.error !== '拒绝访问!') {
      message = req.session.error;
    }
  } else {
    message = req.session.success;
  }
  res.render('login', { title: '登录', message: message });
});

router.post('/login', async function (req, res) {
  await authenticate(req.body.username, req.body.password, function (err, user) {
    if (user) {
      req.session.regenerate(function () {
        req.session.user = user;
        req.session.success = '认证成功，用户为 ' + user.name;
        if (user.pass_set) {
          const basePath = req.app && req.app.locals ? (req.app.locals.basePath || '') : '';
          const redirectTarget = req.query.redirect || (basePath + '/controller');
          res.redirect(redirectTarget);
        } else {
          const basePath = req.app && req.app.locals ? (req.app.locals.basePath || '') : '';
          res.redirect(basePath + '/users/' + user.name + '/password');
        }
      });
    } else {
      req.session.error = '身份验证失败，请检查您的用户名和密码.'
      const basePath = req.app && req.app.locals ? (req.app.locals.basePath || '') : '';
      res.redirect(basePath + '/login');
    }
  });
});
module.exports = router;
