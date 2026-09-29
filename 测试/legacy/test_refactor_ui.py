"""Browser checks for the design preview and staff workflow polish."""
import unittest
import test_frontend as base


class RefactorUITests(unittest.TestCase):
    setUpClass = classmethod(base.FrontendTests.setUpClass.__func__)
    tearDownClass = classmethod(base.FrontendTests.tearDownClass.__func__)
    setUp = base.FrontendTests.setUp
    tearDown = base.FrontendTests.tearDown
    admin = base.FrontendTests.admin
    login = base.FrontendTests.login
    go = base.FrontendTests.go

    def test_preview_uses_seed_data_and_operable_states(self):
        self.go('design-preview.html')
        self.page.wait_for_function('() => document.querySelectorAll("#previewResidents tr").length === 4')
        self.assertIn('王国栋', self.page.locator('#previewResidents').inner_text())
        self.page.click('#previewTheme')
        self.assertEqual(self.page.locator('#previewTheme').get_attribute('aria-pressed'), 'true')
        self.page.click('#previewDialog')
        self.assertFalse(self.page.locator('#previewOverlay').is_hidden())
        self.assertEqual(self.page.evaluate('document.activeElement.id'), 'previewCancel')
        self.page.keyboard.press('Escape')
        self.assertTrue(self.page.locator('#previewOverlay').is_hidden())
        self.assertEqual(self.page.evaluate('document.activeElement.id'), 'previewDialog')
        self.page.click('#previewToast')
        self.assertIn('演示数据未改变', self.page.locator('#previewToastMessage').inner_text())
        self.assertFalse(self.errors)

    def test_staff_desktop_navigation_and_export_scope(self):
        self.page.set_viewport_size({'width': 1440, 'height': 900})
        self.admin()
        self.assertTrue(self.page.locator('#menuPanel').is_visible())
        self.assertFalse(self.page.locator('#hamburgerBtn').is_visible())
        self.page.click('[data-view="sharing"]')
        self.page.wait_for_function('() => document.querySelectorAll("#exportResidents input").length > 0')
        self.assertIn('12/12', self.page.locator('#exportScope').inner_text())
        self.page.click('#btnDeselectAll')
        self.assertIn('0/12', self.page.locator('#exportScope').inner_text())
        self.page.fill('#exportDateStart', '2026-09-26')
        self.page.fill('#exportDateEnd', '2026-09-25')
        self.page.click('#btnExport')
        self.assertIn('开始日期不能晚于结束日期', self.page.locator('#exportHint').inner_text())
        self.page.set_viewport_size({'width': 390, 'height': 844})
        self.assertTrue(self.page.locator('#hamburgerBtn').is_visible())
        self.page.click('#hamburgerBtn')
        self.page.click('[data-view="browse"]')
        self.page.wait_for_selector('.browse-card')
        self.assertFalse(self.errors)

    def test_preview_and_apps_at_200_percent_zoom(self):
        self.page.set_viewport_size({'width': 768, 'height': 900})
        for path in ('design-preview.html', 'home.html', 'index.html'):
            self.go(path)
            self.page.evaluate('document.documentElement.style.zoom="200%"')
            width = self.page.evaluate('document.documentElement.scrollWidth')
            self.assertLessEqual(width, 768, (path, width))
            self.assertFalse(self.errors, (path, self.errors))

    def test_resident_home_actions_are_clear_of_bottom_navigation(self):
        self.login()
        positions = self.page.evaluate('''() => {
            const nav = document.querySelector('.bottom-nav').getBoundingClientRect();
            const buttons = [...document.querySelectorAll('#tab-home .home-actions button')];
            return {navTop:nav.top,buttons:buttons.map(button => {
                const box=button.getBoundingClientRect();
                return {top:box.top,bottom:box.bottom,height:box.height};
            })};
        }''')
        self.assertTrue(all(button['bottom'] < positions['navTop'] and button['height'] >= 48
                            for button in positions['buttons']), positions)
        self.assertFalse(self.errors)

    def test_staff_mobile_controls_have_separate_rows_and_real_feedback(self):
        self.page.set_viewport_size({'width': 320, 'height': 844})
        self.admin()
        layout = self.page.evaluate('''() => {
            const box = id => document.getElementById(id).getBoundingClientRect();
            const logout=box('btnAdminLogout'), monitor=box('simToggle');
            const wander=box('btnSimWander'), danger=box('btnSimDanger');
            return {scrollWidth:document.documentElement.scrollWidth,
              logoutBottom:logout.bottom,monitorTop:monitor.top,
              monitorBottom:monitor.bottom,wanderTop:wander.top,
              dangerTop:danger.top,wanderHeight:wander.height,dangerHeight:danger.height,
              dangerRight:danger.right};
        }''')
        self.assertLessEqual(layout['scrollWidth'], 320, layout)
        self.assertLess(layout['logoutBottom'], layout['monitorBottom'], layout)
        self.assertLess(layout['monitorBottom'], layout['wanderTop'], layout)
        self.assertAlmostEqual(layout['wanderTop'], layout['dangerTop'], delta=1)
        self.assertGreaterEqual(layout['wanderHeight'], 44)
        self.assertGreaterEqual(layout['dangerHeight'], 44)
        self.assertLessEqual(layout['dangerRight'], 320)
        self.page.locator('#btnSimDanger').focus()
        self.assertEqual(self.page.evaluate('document.activeElement.id'), 'btnSimDanger')
        self.page.click('#hamburgerBtn')
        self.page.click('[data-view="browse"]')
        self.assertEqual(self.page.evaluate('document.activeElement.id'), 'topbarTitle')
        self.assertEqual(self.page.locator('[data-view="browse"]').get_attribute('aria-current'), 'page')
        self.page.click('#hamburgerBtn')
        self.page.click('[data-view="summary"]')
        self.page.click('#btnSimDanger')
        self.page.wait_for_selector('#dangerModal:not([hidden])')
        self.assertFalse(self.errors)

    def test_export_choices_are_touch_sized_and_keep_selection_feedback(self):
        self.page.set_viewport_size({'width': 390, 'height': 844})
        self.admin()
        self.page.click('#hamburgerBtn')
        self.page.click('[data-view="sharing"]')
        first = self.page.locator('#exportResidents label').first
        self.assertGreaterEqual(first.bounding_box()['height'], 44)
        first.click()
        self.assertFalse(first.locator('input').is_checked())
        self.assertIn('11/12', self.page.locator('#exportScope').inner_text())
        first.click()
        self.assertTrue(first.locator('input').is_checked())
        self.assertIn('12/12', self.page.locator('#exportScope').inner_text())
        pdf = self.page.locator('#view-sharing .radio').filter(has_text='PDF')
        self.assertGreaterEqual(pdf.bounding_box()['height'], 44)
        pdf.click()
        self.assertTrue(self.page.locator('input[name="exportFormat"][value="pdf"]').is_checked())
        self.assertFalse(self.errors)

    def test_dark_theme_has_neutral_surfaces_and_readable_primary_actions(self):
        entries = [
            (lambda: self.go('home.html'), '.btn--primary'),
            (lambda: self.go('login.html'), '.login-btn-cn'),
            (self.login, '#tab-home .btn-primary'),
            (self.admin, '#btnAdmit'),
        ]
        for open_page, selector in entries:
            open_page()
            self.page.evaluate("YiheTheme.apply('dark')")
            self.page.wait_for_timeout(400)
            colors = self.page.evaluate('''selector => {
              const body = getComputedStyle(document.body);
              const button = getComputedStyle(document.querySelector(selector));
              return {body:body.backgroundColor,foreground:button.color,button:button.backgroundColor};
            }''', selector)
            def channels(value):
                return [int(part.strip()) for part in value.split('(')[1].split(')')[0].split(',')[:3]]
            def luminance(rgb):
                linear = [v / 255 / 12.92 if v / 255 <= .04045
                          else ((v / 255 + .055) / 1.055) ** 2.4 for v in rgb]
                return sum(a * b for a, b in zip(linear, (.2126, .7152, .0722)))
            body = channels(colors['body'])
            self.assertLessEqual(max(body) - min(body), 5, colors)
            a, b = luminance(channels(colors['foreground'])), luminance(channels(colors['button']))
            self.assertGreaterEqual((max(a, b) + .05) / (min(a, b) + .05), 4.5, colors)
            self.assertFalse(self.errors)
