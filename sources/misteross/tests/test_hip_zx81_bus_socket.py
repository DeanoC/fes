from __future__ import annotations

import unittest
from pathlib import Path

from scripts import hip_zx81_bus_socket, pnr_recipe


class HipZx81BusSocketTests(unittest.TestCase):
    def test_route_uses_the_production_zx81_pin_assignment_policy(self) -> None:
        recipe = pnr_recipe.recipe_for("fes.zx81")

        self.assertTrue(recipe["flags"]["arrival_pin_assignment"])
        self.assertEqual(
            hip_zx81_bus_socket.socket_route_extra(Path("/missing/nextpnr-mistral")),
            ("--router", "gpu", "--arrival-pin-assignment"),
        )


if __name__ == "__main__":
    unittest.main()
